# Gopkin — Cross-Platform Plug-and-Play UI Framework (Pure Go)

> **Vision:** Flutter ki cross-platform speed + FilamentPHP ka plug-and-play simplicity + Pure-Go Native Backend (Rock-Solid Local SQL & Live MySQL) + IoT/Hardware readiness — **0 se Pure Go mein**, zero 3rd-party UI dependencies aur minimum code ke saath.

---

## 1. Brand Identity & Website Theme (Monochrome)

Gopkin ki official website (Flutter.dev style) aur default UI theme strictly **Black & White** minimalist aesthetic follow karegi, logo ke hisaab se:

- **Primary Black:** `#000000` (Headings, primary buttons, dark surfaces, logo text)
- **Pure White:** `#FFFFFF` (Main background, light surfaces, primary button text)
- **Accent Grey (Logo Dot):** `#B5B5B5` (Subtle highlights, borders, active dot indicators, muted text)
- **Surface Grey:** `#F5F5F5` (Light mode cards/code blocks) / `#111111` (Dark mode cards)
- **Typography:** Clean geometric sans-serif (Inter / Plus Jakarta Sans / Geist) + crisp monospace for 1-liner code previews.
- **Website Sections (Flutter.dev Style):** Hero with 1-liner Gopkin code preview, Multi-platform showcase (Mobile, Desktop, Web, IoT), Filament-style Plug-and-Play live demo, Dual-SQL Native Backend section, aur Quick Start (`gopkin new app`).

---

## 2. Core Pillars (USP)

1. **100% Pure Go Stack (Go + Go):** Core UI engine, widgets, state, CLI (`gopkin`), aur native backend sab Go mein. Sirf OS window/GPU surface ke liye minimal native C/OS syscalls.
2. **1-Liner Declarative Syntax:** Koi lamba boilerplate nahi. Popups, Forms, Tables, aur Dashboards 1–10 lines mein ready.
3. **True Cross-Platform:** Ek codebase se Desktop (Windows/macOS/Linux), Mobile (Android/iOS), Web (SEO-friendly HTML/CSS), aur Embedded Linux / IoT screens.
4. **Rock-Solid Database Layer (No Flutter Local-DB Pain):**
   - **Local Embedded SQL (`db.LocalSQL`):** Mobile, Desktop, aur IoT par 100% consistent file-based SQL. Automatic schema migrations, full SQL support, aur offline-first storage.
   - **Direct Live MySQL (`db.MySQL`):** Local MySQL (XAMPP/WAMP/Laragon) ya Live Cloud MySQL se seedha connection.
   - **Secure Auto-Proxy (`db.MySQLProxy`):** Public Android/iOS apps mein live MySQL credentials expose na hon, iske liye `gopkin serve --mysql-proxy` ek lightweight Go secure bridge auto-run karega.
   - **External Backend Mode:** Kisi bhi REST, GraphQL, ya WebSocket API (Laravel, Node, Python) se instant connect.
5. **IoT & Hardware Ready:** Core lightweight rahega; hardware ke liye pluggable Go packages (`iot/gpio`, `iot/mqtt`, `iot/serial`, `iot/ble`, Linux Framebuffer/DRM).

---

## 3. System Architecture (4 Layers)

```text
+-------------------------------------------------------------------+
|  Layer 4: Plug-and-Play Kit (Filament Style)                      |
|  Resource, Table, Form, Auth, Dashboard, Charts, Modals, Alerts   |
+-------------------------------------------------------------------+
|  Layer 3: Data & Pure-Go Native Backend                           |
|  LocalSQL (Mobile/Desktop/IoT) | Live MySQL | External REST/WS    |
+-------------------------------------------------------------------+
|  Layer 2: Declarative UI & State Core (Pure Go)                   |
|  1-Liner Widgets, Reactive State, Router, Theme, Layout Engine    |
+-------------------------------------------------------------------+
|  Layer 1: Zero-3rd-Party Platform & GPU Bridge                    |
|  Win32/DX | Metal | Vulkan/JNI | Linux DRM/FB | Web HTML/CSS      |
+-------------------------------------------------------------------+
```

---

## 4. Target Developer Experience (API Preview)

### 1-Liner App & Popup
```go
func main() {
    gopkin.Run("Hello Gopkin",
        ui.Btn("Click Me").OnClick(func() {
            ui.Popup("Success", "Action completed!")
        }),
    )
}
```

### Database Modes (Zero Config Friction)
```go
// Mode 1: Local Device Storage SQL (Works identically on Android, iOS, Desktop, IoT)
db.Connect(db.LocalSQL("app_data.db"))

// Mode 2: Direct Local/Live MySQL (XAMPP/Laragon/Cloud MySQL)
db.Connect(db.MySQL("user:pass@tcp(live-host.com:3306)/myapp"))

// Mode 3: Secure Live MySQL via Auto-Proxy (Recommended for public Mobile APKs)
db.Connect(db.MySQLProxy("https://db-bridge.myapp.com", "API_TOKEN"))
```

### Filament-Style CRUD + IoT Control
```go
app.Page("/devices", ui.Table{
    Source: db.Model("devices"),
    Columns: []ui.Col{
        ui.Text("name").Searchable(),
        ui.Toggle("power").OnChange(iot.MQTT("home/light/1")),
        ui.Badge("status"),
    },
    Actions: []ui.Action{
        ui.CreateModal("Add Device"),
        ui.DeleteAction(),
    },
})
```

---

## 5. Phased Build Roadmap (AI Agent Assisted)

### Phase 1: Pure-Go Core Engine & 1-Liner MVP (Weeks 1–4)
- [ ] Go module (`go mod init github.com/yourusername/gopkin`) aur monorepo setup.
- [ ] `core/state`: Generic thread-safe reactive `State[T]`.
- [ ] `core/ui`: 1-liner widgets (`Text`, `Btn`, `Input`, `Toggle`, `Popup`, `Card`) + Layout (`Row`, `Column`, `Center`).
- [ ] `core/platform`: `Driver` interface + minimal OS native window & 2D canvas bridge (zero 3rd-party UI libs).
- [ ] **Deliverable:** Single-binary Desktop demo app jo 15 lines se kam code mein interactive UI chalaye.

### Phase 2: Filament-Style Kit + Dual-SQL Native Backend (Weeks 5–10)
- [ ] Schema-based `ui.Form`, high-performance `ui.Table`, aur Navigation (`Sidebar`, `Tabs`, `BottomNav`).
- [ ] `backend/db`: `db.LocalSQL`, `db.MySQL`, `db.MySQLProxy`, unified Query Builder, aur JWT Auth.
- [ ] `data/`: External REST & WebSocket adapter.
- [ ] **Deliverable:** Complete CRUD Admin Panel jo Local SQL, Live MySQL, aur External API teeno par chale.

### Phase 3: Web (SEO-Friendly), Official Website & IoT (Months 3–5)
- [ ] **Web Target:** Semantic HTML/CSS output (fast load + SEO).
- [ ] **Official Gopkin Website:** Monochrome Black & White (`#000000` / `#FFFFFF` / `#B5B5B5`) docs + showcase site (built with Gopkin Web).
- [ ] **IoT Packages:** `iot/mqtt`, `iot/serial`, `iot/gpio`, `iot/ble` + Linux ARM64 Framebuffer/Kiosk mode.
- [ ] **Deliverable:** Gopkin.dev style website + Raspberry Pi IoT Dashboard.

### Phase 4: Mobile Targets, CLI & Ecosystem (Months 6–12)
- [ ] Android (JNI/Vulkan) aur iOS (Metal/Swift) embedders.
- [ ] **CLI Tool (`gopkin`):** `gopkin new`, `gopkin dev`, `gopkin serve`, `gopkin build --target=android,ios,desktop,web,iot`.
- [ ] Default Monochrome Theme Engine (Light/Dark) + v1.0 release.

---

## 6. Repository Structure

```text
gopkin/
├── cmd/
│   └── gopkin/            # CLI tool (new, dev, build, serve)
├── core/
│   ├── ui/                # 1-liner declarative widgets & layout
│   ├── state/             # Reactive state management
│   ├── router/            # Navigation & deep linking
│   └── platform/          # Native OS window & GPU bridges (zero 3rd-party)
├── kit/                   # Filament-style plug-and-play components
│   ├── table/             # Data tables with search/sort/pagination
│   ├── form/              # Auto-validated forms
│   ├── auth/              # Login/Register/RBAC screens
│   └── dashboard/         # Charts, stats cards, widgets
├── backend/               # Optional Pure-Go native backend
│   ├── db/                # Unified ORM + LocalSQL + Live MySQL + MySQLProxy
│   ├── server/            # HTTP/WebSocket API server
│   └── auth/              # JWT & session authentication
├── data/                  # External API adapters (REST, GraphQL, WS)
├── iot/                   # Pluggable hardware packages (mqtt, gpio, serial, ble)
├── web/                   # Official Gopkin monochrome website & docs
├── examples/              # Sample apps (counter, crud-mysql, offline-sql, iot-kiosk)
└── docs/                  # ROADMAP.md & architecture specs
```

---

## 7. Instructions for AI Coding Agents

1. **One Module at a Time:** Ek baar mein sirf ek checklist item build karwayein.
2. **Zero 3rd-Party UI Rule:** `core/` aur `kit/` mein sirf Go standard library aur minimal OS syscalls use karein.
3. **Strict Go Interfaces First:** `platform.Driver`, `db.Engine`, `data.Source`, aur `ui.Widget` interfaces pehle lock karein.
4. **Automated Tests Mandatory:** Har package ke saath `_test.go` likhwayein aur `go test ./...` pass hone ke baad hi aage badhein.
5. **Monochrome Design System:** Default UI components aur `web/` site strictly `#000000`, `#FFFFFF`, aur `#B5B5B5` palette follow karein.
