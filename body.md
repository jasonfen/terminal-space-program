Fixes from review #555 (lane fix-app). No behaviour change beyond the card margin.

| ID | What I did | Guard + red-first |
|---|---|---|
| M1 | New test through the real keys: pause, menu, Load Game, load, ctrl+c, y; asserts `menuHeld` false and an autosave file lands. (The esc,q,y route would mask it, openMenu re-reads the clock.) | `TestLoadFromThePauseMenuReleasesTheMenuHold`. Red with `a.menuHeld = false` removed: `menuHeld still true after loading...` and `wrote no autosave`. |
| L8 | Literal 40x13 pin (rows, widths, constants). | `TestMenuRendersHighlightAndKeyedFooter`. Red with `MenuCardW = 42`: `card row 0 is 42 cells wide, want 40`. |
| L9 | Replaced the hand-set `fromMenu=false` block: inject a stale flag on the map, press real F1, esc. (M/Missions was tried first and is not load-bearing, its esc does not read the flag.) | `TestBackReturnsToTheScreenThatOpenedYou`. Red with the `app.go` map-key line removed: `Help opened from the map went back to 5, want the map`. |
| L11 | `menuOverMap` leaves one clear cell left/right of the card and a blank row above/below it. | `TestPauseCardKeepsAClearMarginOverTheMap` (map + launch view, 140x40 and 181x49). Red before the change: row above the card showed `─` at cols 49-56. |
| L-mp | Verified real, NOT changed (new rule needed). See comment. | none |

Captures: designdocs `ux-reviews/2026-10-04-cycle3-b11/fixes-fix-app/pause-card-{140x40,181x49}.txt`.
vet + `go test ./... -race -count=1` green (3493) on origin/main + this branch.
