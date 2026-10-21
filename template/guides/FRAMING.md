# Framing: the detailed steps

**Phase 0: framing** (first cycle, no code, **time-boxed by the pilot**: your goal for this cycle gives the time box). Its only product is the plan: everything that can wait for a milestone waits for it.
1. Read `.athanor/REQUEST.md`, the list of its attached files, `.athanor/PC_TOOLS.md` and the solutions files.
2. What you cannot answer from them in a minute (what an existing code base holds, what the mock-ups show, which libraries fit) goes to up to 3 `scout` sub-agents **launched at once**, one question each. Each one answers in at most 40 lines, or writes one document (e.g. the visual description of the mock-ups, colours, fonts and components per screen, into `.athanor/docs/DESIGN.md`). Do not reread after them what they summarised.
3. Write yourself, short and decided, in the workshop:
   - `.athanor/docs/REQUIREMENTS.md`: goal, audience, features ranked (essential / important / bonus), out of scope, measurable success criteria;
   - `.athanor/docs/DESIGN.md`, unless a scout wrote it: what defines the product (for a game: world, story in acts, characters, game loop, mechanics, controls, progression, interface, art and sound direction; for an application: user journeys, screens, data, business rules, visual style);
   - `.athanor/docs/ARCHITECTURE.md`: justified stack (built and tested here with the tools of `.athanor/PC_TOOLS.md` or installable into `$TOOLS`), folders, modules, shared contracts (data shapes, API routes, conventions), test strategy, contents of `check.sh`;
   - `.athanor/PLAN.md`: 8 to 25 milestones, one line each, every line a **self-contained sheet**: `- [ ] M7 : Contact form with validation — Files: contact.html, js/contact.js, tests/contact.spec.ts. Depends on: M1. Acceptance: empty submit shows 5 errors; check.sh quick contact passes. Read: DESIGN.md §Contact, ARCHITECTURE.md §Forms.` A milestone is 20 to 30 minutes of work for someone who reads only its line and what `Read` names. M1 is always the runnable skeleton with `check.sh` and the shared contracts.
4. One `critic` pass on `.athanor/PLAN.md`, briefed as a **plan reviewer** rather than an end user: missing features from the requirements, wrong or missing dependencies, milestones too big or not self-contained, acceptance criteria that cannot be checked. Fix what it found. Once, no loop.

Never in the framing: writing code, installing a tool or testing one (`.athanor/PC_TOOLS.md` says what is there; an installation is milestone work), rendering or measuring the attachments yourself after a scout did, writing **demo content** (sample data, lessons, articles, products, level texts: it is a milestone of its own near the end of the plan). When the time box is spent, the pilot starts the plan as it stands: have `.athanor/PLAN.md` written within the first half of the time box, refine it after.

## Default technical choices (for ARCHITECTURE.md)

If `.athanor/REQUEST.md` imposes nothing, favor what can be built and tested entirely here, without human intervention:
- **Game**: web, TypeScript + Vite, Three.js for 3D or Phaser for 2D. Logic tests with Vitest, browser tests and screenshots with Playwright (through `tool`). If **Unreal Engine** is requested and present in `.athanor/PC_TOOLS.md`, use it (C++ or Blueprints, MCP, `unreal-cmd` for automated tests and screenshots, `unreal-uat` for the executable). Another engine requested: install it in `$TOOLS` and drive it from the command line.
- **Application or website**: web (TypeScript + Vite; React if the interface is rich).
- **Tool, script, automation**: Python or Node.
