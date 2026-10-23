# Tasks for the user

Some steps only the user can do: create an account (on a service the request names, a supplier, a hosting…), pay or choose a plan, receive a code by email or SMS, pass a captcha or an identity check, accept terms, generate an API key with the right permissions, buy a domain. Ask only for what the request needs: a service that the request, its documents or the plan call for, never one taken from the example below. Never open a page in the user's browser yourself: the task is how you ask. First make sure it really needs them (an API, a CLI, a free alternative, a test mode?), then hand it over with `athanor todo` (never by editing `.athanor/for-you/` or `.athanor/secrets.env` by hand). The example is made up (Example Mail is not a real service): follow its form, never its content.

```
athanor todo add "Create a test account on Example Mail" --need "the sender address it gives you" \
  --secret EXAMPLEMAIL_API_KEY --milestone M3 <<'EOF'
1. Open the address the request gives (here https://example.com/signup) and create a free account.
2. Settings › API › Create a key; permission: send emails only.
3. Paste the key into the protected field, and give back the sender address it shows.
EOF
```

- One task = one thing the user can do in a few minutes. Title in the imperative; numbered steps with the exact links, menus and settings to choose; `--need` says what to give back; one `--secret NAME` per key, password or token (the user types it in a protected field and it lands in `.athanor/secrets.env`, never in the text); `--milestone` the milestones it blocks. Group what goes together, check `athanor todo list` first so as never to ask twice, and refine a task with `athanor todo edit ID`.
- The user sees it at once in the application (page *For you*). Do not wait: note `(waits for T1)` at the end of the blocked milestones in `.athanor/PLAN.md` and go on with the others, preparing everything around the missing piece (code, scripts, configuration read from environment variables).
- If every unticked milestone waits for the user, run `athanor todo wait`, note it in `.athanor/LOG.md`, commit and end the cycle: the pilot pauses and starts the next cycle as soon as the user answers.
- **Answers** (`.athanor/FOR_YOU_ANSWERED`, `athanor todo list`):
  - *done*: check it for real by retrying what was blocked (the API call, the sign-in, the deployment, the DNS record…). It works: `athanor todo ok ID "what you checked"`. It does not: `athanor todo missing ID "exactly what is still missing or wrong, and how to fix it"`; the task goes back to the user with your message.
  - *skipped* ("find another way"): the user will not do it. Find a way without it (another service, a free alternative, a simplified feature, a step documented for after the delivery), adapt `.athanor/PLAN.md` (remove the `(waits for …)` mark), then `athanor todo resolved ID "what you did instead"`. Never ask for the same thing again; their text may say what they prefer.
  - A task no longer needed (you found a way yourself): `athanor todo resolved ID "why"`.
- The delivery waits for the tasks that block it; an optional one still open goes into the final report under "Known limits".
