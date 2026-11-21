//go:build windows

package main

// The graphics cards on Windows: their names and own memory from DXGI; how busy they are and the memory they use
// from the system's GPU counters (PDH: "GPU Engine", "GPU Adapter Memory"), the Task Manager's, for every maker
// (NVIDIA, AMD, Intel). No program is started.

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	pdhDLL                        = windows.NewLazySystemDLL("pdh.dll")
	pPdhOpenQueryW                = pdhDLL.NewProc("PdhOpenQueryW")
	pPdhAddEnglishCounterW        = pdhDLL.NewProc("PdhAddEnglishCounterW")
	pPdhCollectQueryData          = pdhDLL.NewProc("PdhCollectQueryData")
	pPdhGetFormattedCounterArrayW = pdhDLL.NewProc("PdhGetFormattedCounterArrayW")
	pCreateDXGIFactory1           = windows.NewLazySystemDLL("dxgi.dll").NewProc("CreateDXGIFactory1")
)

const (
	pdhFmtDouble   = 0x00000200
	pdhFmtLarge    = 0x00000400
	pdhFmtNoCap100 = 0x00008000
	pdhMoreData    = 0x800007D2
)

// pdhElement: PDH_FMT_COUNTERVALUE_ITEM_W: the instance's name, the value's status, the value (a double or a
// LONGLONG, as asked).
type pdhElement struct {
	nom    *uint16
	statut uint32
	_      uint32
	valeur [8]byte
}

// objetCOM: a COM interface, whose first field points to its table of methods.
type objetCOM struct{ vt *[16]uintptr }

// descAdaptateur1: DXGI_ADAPTER_DESC1.
type descAdaptateur1 struct {
	Description                                   [128]uint16
	Fabricant, Peripherique, SousSysteme, Version uint32
	MemoireDediee, SystemeDediee, SystemePartagee uintptr
	LuidBas                                       uint32
	LuidHaut                                      int32
	Drapeaux                                      uint32
}

var iidIDXGIFactory1 = windows.GUID{Data1: 0x770aae78, Data2: 0xf26f, Data3: 0x4dba, Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}

type adaptateurGPU struct {
	cle, nom string
	vram     uint64
}

var gpuWin struct {
	sync.Mutex
	pret                              bool
	adaptateurs                       []adaptateurGPU
	requete, moteurs, dediee, partage uintptr
}

// lireGPU: each graphics card now; how busy it is comes from the second reading on (a rate between two).
func lireGPU() []GPUVue {
	gpuWin.Lock()
	defer gpuWin.Unlock()
	if !gpuWin.pret { // shortcut: the cards are listed once; one plugged in later shows after a restart of Athanor
		gpuWin.pret = true
		gpuWin.adaptateurs = adaptateursDXGI()
		var q uintptr
		if len(gpuWin.adaptateurs) > 0 && pdhDLL.Load() == nil {
			if r, _, _ := pPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&q))); r == 0 {
				gpuWin.requete = q
				gpuWin.moteurs = compteurPDH(q, `\GPU Engine(*)\Utilization Percentage`)
				gpuWin.dediee = compteurPDH(q, `\GPU Adapter Memory(*)\Dedicated Usage`)
				gpuWin.partage = compteurPDH(q, `\GPU Adapter Memory(*)\Shared Usage`)
			}
		}
	}
	if len(gpuWin.adaptateurs) == 0 {
		return nil
	}
	var occupe, dediee, partage map[string]float64
	if gpuWin.requete != 0 {
		if r, _, _ := pPdhCollectQueryData.Call(gpuWin.requete); r == 0 {
			occupe = utilisationMoteurs(valeursPDH(gpuWin.moteurs, pdhFmtDouble|pdhFmtNoCap100))
			dediee, partage = parAdaptateur(valeursPDH(gpuWin.dediee, pdhFmtLarge)), parAdaptateur(valeursPDH(gpuWin.partage, pdhFmtLarge))
		}
	}
	res := make([]GPUVue, 0, len(gpuWin.adaptateurs))
	for _, a := range gpuWin.adaptateurs {
		g := GPUVue{Nom: a.nom, Utilisation: -1, VRAMTotal: a.vram, VRAMUtil: uint64(dediee[a.cle]), Partagee: uint64(partage[a.cle])}
		if len(occupe) > 0 { // the counter answered: a card none of whose engines is listed is idle
			g.Utilisation = occupe[a.cle]
		}
		res = append(res, g)
	}
	return res
}

// compteurPDH adds a counter (English path, wildcard instances) to the query; 0 when the system has none.
func compteurPDH(q uintptr, chemin string) uintptr {
	p, _ := windows.UTF16PtrFromString(chemin)
	var c uintptr
	if r, _, _ := pPdhAddEnglishCounterW.Call(q, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&c))); r != 0 {
		return 0
	}
	return c
}

// valeursPDH: each instance's value of a counter at the last collection (the instances without a valid value left out).
func valeursPDH(c uintptr, format uint32) map[string]float64 {
	res := map[string]float64{}
	if c == 0 {
		return res
	}
	var taille, n uint32
	if r, _, _ := pPdhGetFormattedCounterArrayW.Call(c, uintptr(format), uintptr(unsafe.Pointer(&taille)), uintptr(unsafe.Pointer(&n)), 0); uint32(r) != pdhMoreData || taille == 0 {
		return res
	}
	buf := make([]uint64, (taille+7)/8) // the items, then their names: 8-byte aligned
	if r, _, _ := pPdhGetFormattedCounterArrayW.Call(c, uintptr(format), uintptr(unsafe.Pointer(&taille)), uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&buf[0]))); r != 0 {
		return res
	}
	n = min(n, uint32(uintptr(len(buf))*8/unsafe.Sizeof(pdhElement{})))
	for _, e := range unsafe.Slice((*pdhElement)(unsafe.Pointer(&buf[0])), n) {
		if e.statut > 1 { // PDH_CSTATUS_VALID_DATA or PDH_CSTATUS_NEW_DATA only
			continue
		}
		v := *(*float64)(unsafe.Pointer(&e.valeur))
		if format&pdhFmtLarge != 0 {
			v = float64(*(*int64)(unsafe.Pointer(&e.valeur)))
		}
		res[windows.UTF16PtrToString(e.nom)] += v
	}
	runtime.KeepAlive(buf)
	return res
}

// parAdaptateur adds up the instances of each adapter (its physical parts, phys_0, phys_1…).
func parAdaptateur(valeurs map[string]float64) map[string]float64 {
	res := map[string]float64{}
	for inst, v := range valeurs {
		if k := cleAdaptateur(inst); k != "" {
			res[k] += v
		}
	}
	return res
}

// adaptateursDXGI: the graphics cards (IDXGIFactory1::EnumAdapters1, then GetDesc1), without Microsoft's software one.
func adaptateursDXGI() []adaptateurGPU {
	if pCreateDXGIFactory1.Find() != nil {
		return nil
	}
	var f *objetCOM
	if r, _, _ := pCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iidIDXGIFactory1)), uintptr(unsafe.Pointer(&f))); r != 0 || f == nil {
		return nil
	}
	defer syscall.SyscallN(f.vt[2], uintptr(unsafe.Pointer(f))) // Release
	var res []adaptateurGPU
	for i := 0; i < 16; i++ {
		var a *objetCOM
		if r, _, _ := syscall.SyscallN(f.vt[12], uintptr(unsafe.Pointer(f)), uintptr(i), uintptr(unsafe.Pointer(&a))); r != 0 || a == nil {
			break // DXGI_ERROR_NOT_FOUND: no more cards
		}
		var d descAdaptateur1
		r, _, _ := syscall.SyscallN(a.vt[10], uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&d)))
		syscall.SyscallN(a.vt[2], uintptr(unsafe.Pointer(a)))
		if r != 0 || d.Drapeaux&2 != 0 || d.Fabricant == 0x1414 { // DXGI_ADAPTER_FLAG_SOFTWARE, Microsoft's Basic Render Driver
			continue
		}
		vram := uint64(d.MemoireDediee)
		if r := vramPilote(d.Fabricant, d.Peripherique); r > vram {
			vram = r
		}
		res = append(res, adaptateurGPU{cle: fmt.Sprintf("luid_0x%08x_0x%08x", uint32(d.LuidHaut), d.LuidBas), nom: nomGPU(windows.UTF16ToString(d.Description[:])), vram: vram})
	}
	return res
}

// vramPilote: the card's memory as its driver wrote it (HardwareInformation.qwMemorySize, the Task Manager's), 0
// if none: DXGI leaves out what the driver keeps for itself (7899 MiB of an 8 GB card).
func vramPilote(fabricant, peripherique uint32) uint64 {
	const classe = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}` // display adapters
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, classe, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return 0
	}
	defer k.Close()
	noms, _ := k.ReadSubKeyNames(-1)
	id := fmt.Sprintf(`pci\ven_%04x&dev_%04x`, fabricant, peripherique)
	for _, n := range noms {
		s, err := registry.OpenKey(k, n, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		m, _, _ := s.GetStringValue("MatchingDeviceId")
		taille, _, err := s.GetIntegerValue("HardwareInformation.qwMemorySize")
		s.Close()
		if err == nil && strings.HasPrefix(strings.ToLower(m), id) {
			return taille
		}
	}
	return 0
}
