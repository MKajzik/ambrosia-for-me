# Testing the iOS app on your own iPhone (no paid Apple Developer account)

You do **not** need the paid Apple Developer Program, TestFlight or a Team ID you bought. A free Apple ID is enough: Xcode gives it a **Personal Team** (it has a Team ID too, found in step 6). Without any Apple ID you can still test in the Simulator (last section), but not on a phone.

What this gets you: the Debug build of the app, running on your phone, talking to the API and database running on your Mac over your home Wi-Fi.

What it does not: App Store / TestFlight distribution, Release builds (they stop at launch until a production API URL is set), and Sign in with Apple (not built yet, #29).

## What you need

- A Mac with **Xcode 26** (the project targets iOS 26) and **XcodeGen** (`brew install xcodegen`).
- An iPhone running **iOS 26 or later**, a USB cable, and **Developer Mode** (step 7).
- Docker (for local Postgres), Go 1.26 — the same as for the rest of the repo (`CLAUDE.md`).
- The Mac and the iPhone on the **same Wi-Fi network**. Guest networks and some routers isolate devices from each other ("client isolation"); if step 4 fails, that is the first suspect.
- A free Apple ID.

## 1. Start the backend on your Mac

```sh
cp .env.example .env        # once
make db-up                  # Postgres in Docker
make migrate                # create the tables
make run-api                # API on :8080; leave this terminal open
```

`make run-api` listens on all network interfaces, which is what lets the phone reach it. The first time, macOS may ask whether `api` may accept incoming connections: choose **Allow** (if you already denied it: System Settings > Network > Firewall > Options, and allow `api`).

Ingredient search is empty until the USDA data is imported (`FDC_API_KEY=<your key> make import-usda`; a free key is at fdc.nal.usda.gov, `DEMO_KEY` works at a low rate limit). You can also skip it and create your own ingredients in the app (Profile > My ingredients, or from the meal editor).

## 2. Find your Mac's address

Either of these works; the name survives your router handing out a new address.

- The Bonjour name: `scutil --get LocalHostName` prints e.g. `Marcins-MacBook-Pro`, so the host is `Marcins-MacBook-Pro.local`.
- The LAN address: `ipconfig getifaddr en0` (Wi-Fi; try `en1` if empty), e.g. `192.168.1.23`.

## 3. Check the phone can reach the API

On the iPhone, open Safari and go to `http://<your-host>:8080/v1/healthz` (for example `http://192.168.1.23:8080/v1/healthz`). A short JSON "ok" means the network path is fine. If the page does not load, fix this before touching Xcode: same Wi-Fi? Firewall prompt allowed? Router client isolation off? VPN off on either device?

## 4. Tell the app where the API is

```sh
cp ios/Config/Local.xcconfig.example ios/Config/Local.xcconfig
```

Edit `ios/Config/Local.xcconfig` (it is gitignored, so nothing here is committed) and set:

```
API_HOST = 192.168.1.23:8080       # or Marcins-MacBook-Pro.local:8080
```

Leave `DEVELOPMENT_TEAM` for step 6. The app allows plain `http` only to local addresses (IP addresses and `.local` names), so this works without HTTPS; anything else on the internet still requires HTTPS.

## 5. Add your Apple ID to Xcode

Xcode > Settings > Accounts > **+** > Apple ID, and sign in with your normal (free) Apple ID. A team named **"Your Name (Personal Team)"** appears.

## 6. Generate the project and pick the team once

```sh
cd ios && xcodegen generate && open MealPlanner.xcodeproj
```

In Xcode: select the **MealPlanner** project > the **MealPlanner** target > **Signing & Capabilities** > tick **Automatically manage signing** > Team: choose your **Personal Team**.

If Xcode says the bundle identifier `dev.ambrosiaforme.mealplanner` is not available, someone else registered it. Add your own to `Local.xcconfig`, e.g. `APP_BUNDLE_ID = com.yourname.mealplanner`, then run `xcodegen generate` again.

Now read your Team ID so it survives regenerating the project (`xcodegen generate` rewrites the Xcode project and forgets the team you picked in the UI):

```sh
grep -m1 DEVELOPMENT_TEAM ios/MealPlanner.xcodeproj/project.pbxproj
```

Copy the 10-character value into `ios/Config/Local.xcconfig` as `DEVELOPMENT_TEAM = XXXXXXXXXX`, run `xcodegen generate` once more, and reopen the project.

## 7. Prepare the iPhone

1. Plug it into the Mac and unlock it. Tap **Trust** on the phone when asked, and enter the passcode.
2. **Settings > Privacy & Security > Developer Mode** > on. The phone restarts; confirm again after the restart. (The switch only appears after the phone has been connected to Xcode.)

## 8. Run the app

In Xcode, pick your iPhone in the device menu at the top (next to the **MealPlanner** scheme) and press **Run** (⌘R). The first build takes a few minutes. If the phone shows "Untrusted Developer":

**Settings > General > VPN & Device Management** > your Apple ID under "Developer App" > **Trust**, then open the app again.

On first launch iOS asks to allow **Local Network** access: tap **Allow**. (If you tapped Don't Allow: Settings > Meal Planner > Local Network.) Without it the app cannot reach the API and shows "Can't reach the server".

Later runs can be wireless: Window > Devices and Simulators > select the phone > "Connect via network".

## 9. What to try

Register an account in the app (any email, password of 10+ characters), then walk through the product:

| Area | Try |
|---|---|
| Meals | Create a meal, add ingredients (search or create your own), change servings, check the nutrition panel, autosave |
| Plan / Today | Put meals on dates, change a portion, set targets in Profile and watch the rings on Today, create a diet template and apply it to a week |
| Shopping | Generate a list from the plan, check items off, add one, remove one; turn on Airplane Mode, check items, turn it off and watch the syncing badge clear |
| Profile | Targets (blank clears one), account deletion (asks for your email) |
| Partner | Needs two accounts: register a second one in the Simulator or on the web app (`make run-web`), create an invite code on one and enter it on the other. Partner meals and diets are read-only and copyable; shopping lists shared by the owner update live on both |

## Limits of free provisioning

- The app **expires after 7 days** and will not open. Press Run from Xcode again to renew it (data in the app is kept while it is the same install).
- You can only have a few free app IDs per week and 3 apps installed this way at once.
- The phone must be able to see the Mac to be useful: no Mac running `make run-api`, no data. Nothing is hosted anywhere.
- This is a Debug build: slower than a store build, and it shows nothing about App Store review.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| "Can't reach the server" on the phone | Step 3 fails too: wrong host, different Wi-Fi, firewall, client isolation, or Local Network permission denied. Check `API_HOST`, then `xcodegen generate` and run again (the URL is baked in at build time). |
| Safari works but the app does not | The app was built before you edited `Local.xcconfig`; regenerate and rebuild. Also check the Local Network permission. |
| "Failed to register bundle identifier" | Set `APP_BUNDLE_ID` to your own (step 6). |
| "No Account for Team" / "Signing requires a development team" | `DEVELOPMENT_TEAM` empty or wrong, or your Apple ID is not signed in (steps 5-6). |
| "Developer Mode disabled" | Step 7.2. |
| "Unable to install ... iOS 26 required" | The phone is older than iOS 26. |
| App opens then closes after about a week | The 7-day expiry; Run again from Xcode. |
| Keychain / sign-in loops after changing the bundle id | A new bundle id is a new app with its own data; sign in again. |
| Port 8080 busy | Another process holds it: `lsof -nP -iTCP:8080 -sTCP:LISTEN`, stop it, restart `make run-api`. |
| Everything worked, now nothing loads | The Mac's address changed or the Mac slept. Use the `.local` name, and keep the Mac awake while testing. |

## No iPhone, or no Apple ID: the Simulator

The Simulator needs no account and reaches the API at `localhost`, so skip steps 2-7:

```sh
make run-api                # one terminal
make build-ios              # builds for the Simulator
open ios/MealPlanner.xcodeproj   # pick an iPhone simulator, Run
```

Unit tests need neither: `make test-ios`.
