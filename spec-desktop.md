# AgentBoard — Desktop App Spec

> **Status**: Brainstorm, not a plan. Explores wrapping the AgentBoard binary in a Tauri shell so users get a native app experience while the CLI + local server stay first-class. Picks a preferred architecture in §5 and lists the open decisions in §10. Additive — no change to the single-binary story; the desktop app is a *third* surface alongside the CLI and the hosted URL.

---

## 1. Motivation

AgentBoard today is "a binary you run, then a browser tab you manage." That works, but it leaks friction:

- The dashboard is one tab among twenty. It gets lost behind Gmail.
- Users forget whether the server is running; there's no dock presence, no menubar indicator.
- The project folder is a CLI flag buried in a terminal the user has to remember they spawned.
- A non-technical user who installs AgentBoard still has to meet the server halfway at `localhost:3000`.
- Running against a hosted instance means bookmarking a URL and typing a token into a header that the browser immediately forgets.

A thin desktop wrapper collapses all of that into one icon that:

1. Launches the server for you (or doesn't, if you already have one running).
2. Opens the dashboard in a dedicated window, not a browser tab.
3. Remembers which project/instance you used last.
4. Can also point at a **remote** AgentBoard — same UI, different backend, one token stored in the OS keychain.

Crucially, **the CLI and the Go binary don't change**. The desktop app is a shell that invokes the same binary as a sidecar. Power users keep their terminal workflow; new users get a double-clickable app.

---

## 2. Design principles

1. **The binary is the product; the app is the chrome.** No logic lives in the app that couldn't also be done from the CLI. If a feature only works in the desktop app, something is wrong.
2. **Local and remote are the same UI.** The webview doesn't care if the server is `127.0.0.1:3000` or `dash.example.com`. Switching instances is a settings toggle, not a separate app.
3. **No forked frontend.** The React app ships embedded in the Go binary (local mode) or served by the remote instance (remote mode). The desktop app never bundles its own copy of the frontend.
4. **Secrets in the OS keychain, never in a config file.** Auth tokens live in Keychain/Credential Manager/Secret Service. A plaintext `~/.agentboard/config.json` is fine for non-secret prefs.
5. **Offline-friendly for local mode, graceful for remote.** Local mode must work with zero network. Remote mode must show a clear "can't reach server" state instead of a blank webview.
6. **Do not reinvent auth.** The existing `AGENTBOARD_AUTH_TOKEN` scheme is the auth scheme. The app just stores and injects the token.

---

## 3. Non-goals

- **Not an Electron app.** We don't want a 120MB binary for a 20MB Go server. Tauri (or equivalent WebView-based shell) only.
- **No app-level data store.** The app does not cache dashboards, replicate state, or do offline writes. If the server isn't reachable, the UI is empty or stale — that's honest.
- **No desktop-only components.** A `<DesktopNotification>` that only works in the app would split the component contract. Use the same SSE + beacon channels the browser already has.
- **No cross-instance aggregation** in Phase 1. "Show metrics from three instances on one page" is a future product; the app only talks to one instance at a time.
- **No in-app code editing.** The app is a reader/controller. Authoring stays in the user's editor + Claude Code, same as today.
- **No auto-update from an untrusted channel.** Either we code-sign and ship through a proper update feed, or the app tells the user to download a new version. No silent fetch-and-run of arbitrary binaries.

---

## 4. The two modes

The app has exactly two modes, chosen at launch:

### Mode L — **Local** (default)

- App spawns `agentboard serve --project <dir> --port <auto> --no-open` as a child process.
- Picks a free port on localhost. Passes it to the webview.
- Webview loads `http://127.0.0.1:<port>`.
- Auth is off by default in local mode; loopback-only binding is the trust boundary (same as today).
- When the app quits, the sidecar is killed.

### Mode R — **Remote**

- App reads a saved instance URL + token from the keychain.
- Webview loads `https://<instance>` directly — everything is same-origin from the webview's perspective.
- The token is injected so the session is authenticated. Three candidate mechanisms (§6).
- No sidecar is spawned.

A user can toggle between modes from the app menu. The last-used mode is remembered per-launch.

---

## 5. Architecture — Tauri sidecar model (recommended)

```
┌────────────────────────────────────────────┐
│  Tauri app (Rust shell, ~10MB)             │
│  ┌──────────────────────────────────────┐  │
│  │ WebView (system WKWebView/WebView2)  │  │
│  │ → http://127.0.0.1:<port>  (Mode L)  │  │
│  │ → https://instance.example (Mode R)  │  │
│  └──────────────────────────────────────┘  │
│  ┌──────────────────────────────────────┐  │
│  │ Sidecar: agentboard binary (Mode L)  │  │
│  │   serves frontend + API + MCP + SSE  │  │
│  └──────────────────────────────────────┘  │
│  ┌──────────────────────────────────────┐  │
│  │ Native menu, tray icon, keychain,    │  │
│  │ file picker, deep-link handler       │  │
│  └──────────────────────────────────────┘  │
└────────────────────────────────────────────┘
```

### Why Tauri over Electron

| | **Tauri** | **Electron** |
|---|---|---|
| Shell size | ~10 MB | ~100 MB |
| Runtime | System webview (WKWebView on macOS, WebView2 on Win, WebKitGTK on Linux) | Bundled Chromium |
| Shell language | Rust | Node/JS |
| Sidecar support | First-class (`tauri.conf.json > bundle.externalBin`) | Third-party (`electron-builder`) |
| Auto-update | Built-in (signed update manifest) | `autoUpdater` |
| Matches our ethos | Tiny, single-binary-adjacent | Heavy, bundles a second runtime |

The one place Electron wins is rendering consistency — we'd test against one Chromium, not three webviews. For a docs/dashboard UI that's already deliberately simple, that's an acceptable cost.

### Why sidecar over "app spawns server via shell"

A sidecar declared in `tauri.conf.json` is:
- Bundled into the signed app binary (no PATH surprises).
- Cross-platform pathed automatically (`agentboard-x86_64-apple-darwin`, etc.).
- Killed when the app quits (no zombie servers).
- Sandbox-friendly under macOS app-sandbox rules.

---

## 6. Remote mode — token injection

Three candidates for how the app authenticates the webview to a remote instance. Rank from simplest to most robust:

### 6a. **Query param on initial load**
- On first navigation: `https://instance.example/?token=<stored>`.
- Server returns `Set-Cookie: agentboard-session=<token>; HttpOnly; SameSite=Strict`.
- Subsequent requests ride the cookie.
- **Pros**: Zero frontend changes; server already accepts `?token=`.
- **Cons**: Token appears in the initial URL (not logged by us, but could leak via referrers on outbound links). Needs server-side cookie-setting on token query (small server change).

### 6b. **Authorization header via webview request interceptor**
- Tauri can intercept `fetch`/navigation requests and inject headers.
- Every request gets `Authorization: Bearer <token>` automatically.
- **Pros**: No URL leakage; no cookie; no server change.
- **Cons**: Tauri's webview interception APIs differ per platform. SSE (EventSource) doesn't support custom headers natively — we'd need to use fetch-based SSE or polyfill. Same for the MCP transport.

### 6c. **Inline token in JS context (`Tauri.init` bridge)**
- Inject a small script before page load that stashes the token in `window.__AGENTBOARD_TOKEN__`.
- Frontend fetch/SSE code reads it and adds `Authorization` headers.
- **Pros**: Clean, explicit, testable.
- **Cons**: Frontend *does* need to change — the existing same-origin assumption breaks. All 3 transports (REST, SSE, MCP) must learn the header. This is the path that unlocks true remote mode from the frontend forward.

**Recommendation**: ship **6a** first (smallest change, gets us to a working remote app), plan **6c** as the long-term answer. **6b** is a neat trick but the SSE gap makes it a trap.

---

## 7. Settings & state

The app owns a small amount of local state:

```
~/Library/Application Support/AgentBoard/   (macOS)
%APPDATA%/AgentBoard/                        (Windows)
~/.config/AgentBoard/                        (Linux)
  └─ config.json
     {
       "mode": "local" | "remote",
       "local": {
         "lastProjectDir": "/Users/chris/dev/agentboard",
         "port": 0                            // 0 = auto
       },
       "remote": {
         "lastInstance": "https://dash.example.com",
         "knownInstances": ["https://...", "https://..."]
       },
       "window": { "width": 1400, "height": 900, "x": 100, "y": 100 }
     }
```

Tokens live in the keychain under `agentboard/<instance-hostname>`, **never** in `config.json`.

---

## 8. Native affordances the app enables

These are the reasons the app is more than just a bookmark:

- **Dock/menubar icon** with a subtle state dot (green = connected, amber = reconnecting, red = down).
- **Global hotkey** to raise the window (e.g. ⌘⇧A). Optional, opt-in.
- **Native file picker** for "Open project folder…" — drops the CLI flag for non-technical users.
- **Deep links**: `agentboard://open?instance=https://...&page=/features/auth` lets a Claude response include a link that opens the app on the right page.
- **OS notifications** when the server's beacon reports a new error (bridged through SSE — no server change needed).
- **Launch-at-login** toggle. Off by default.
- **"Open in browser"** menu item for users who *do* want a tab.
- **Multi-window**: a second window can point at a different instance (same app, two dashboards side-by-side). Stretch goal.

---

## 9. Distribution & trust

This is the part that decides whether the app is shippable or a side project:

- **macOS**: code-signed + notarized DMG. $99/yr Apple developer account. Without this, Gatekeeper blocks the app for every user who downloads it. Non-negotiable for a real release.
- **Windows**: signed MSI or MSIX. Code-signing certs are ~$200-400/yr. Without it, SmartScreen flags the download.
- **Linux**: AppImage for portability; `.deb` / `.rpm` / Flatpak if demand warrants. Less signing ceremony.
- **Auto-update**: Tauri's updater consumes a signed JSON manifest. We host the manifest on the same CDN as the landing page. Updates are opt-in on first launch.
- **Homebrew cask** for macOS: `brew install --cask agentboard`. Piggybacks on the existing `brew install agentboard` (CLI) — two casks, one source of truth.

**Open question**: do we ship the desktop app and the CLI as one install (cask + formula) or two? Bundling means the CLI is always available when the app is installed. Separating means a headless server user doesn't have to download a GUI.

---

## 10. Open decisions

- **Tauri 1.x or 2.x?** 2.x is the current line, has better mobile support, less ecosystem maturity. Probably 2.x — we'd be adopting late enough that it's stable.
- **Which auth injection path for remote mode?** See §6. Decision gate: do we want the frontend to become transport-agnostic now (6c, more work, cleaner future) or ship a quick win (6a, reversible)?
- **Ship a tray/menubar mini-UI?** A popover that shows "N beacons, last update 2m ago, [Open dashboard]" without opening the full window. Nice, but scope creep for Phase 1.
- **Do we support "no local binary installed"?** If the user installs the desktop app without the CLI, should the app be usable in remote-only mode? Leaning yes — it's the better onboarding story for non-technical remote users.
- **Sandboxing on macOS** — if we enable the App Sandbox, spawning a sidecar and letting it bind to a port requires specific entitlements. If we skip sandboxing, notarization still works but we lose Mac App Store distribution. Probably fine to skip for now.
- **What does Mode L do if port 3000 is already in use by a separately-launched `agentboard` CLI?** Three options: (a) app picks a different port and spawns its own, (b) app detects the existing server and attaches to it, (c) app refuses to start. **(b)** is the friendly answer but requires a "discover running instance" handshake. **(a)** is the correct default for Phase 1.

---

## 11. Phasing

**Phase 1 — "It's an app"**
- Tauri shell + sidecar, Mode L only.
- Native menu, file picker for project dir, window state persistence.
- macOS signed build, Windows unsigned prerelease, Linux AppImage.
- Ship as a separate download; CLI remains primary.

**Phase 2 — "It talks to the cloud"**
- Mode R with option 6a (query-param + session cookie).
- Keychain token storage.
- Known-instances list in settings.
- Deep-link handler (`agentboard://`).

**Phase 3 — "It's a first-class surface"**
- Option 6c (frontend transport-agnostic) so SSE and MCP ride explicit auth headers.
- Global hotkey, tray popover, OS notifications bridging beacon errors.
- Multi-window, multi-instance side-by-side.
- Homebrew cask; code-signing on all three platforms.
- Auto-update via signed manifest.

---

## 12. What we deliberately keep out of scope forever

- App-side data processing, caching, or offline writes. The binary is the source of truth; the app is a window onto it.
- Shipping a different frontend for the desktop app than for the browser. One codebase, one output.
- Desktop-exclusive components or features. If it can't work in a browser tab, it doesn't belong in AgentBoard.
- Bundling Chromium. If Tauri's webview model stops being viable, the answer is a different small shell (Wails, Neutralino), not Electron.

---

## 13. Success criteria

We'd know the desktop app earned its place if:

- A non-technical user can install the app, pick a folder, and see a dashboard without ever touching a terminal.
- A power user installs the app *and* keeps using `agentboard serve` from the CLI with zero conflict.
- A hosted-instance user opens the app, pastes a URL + token once, and the dashboard survives restarts/reboots without re-auth.
- The desktop app is strictly a *superset* of the browser experience — nothing the browser does is broken, and a few things work better (notifications, deep links, persistent dock presence).

If any of those fails, the app is a distraction from the binary and we should cut it.
