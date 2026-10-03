# Simulates a live Dota match so the overlay can be tested without playing.
#
# TheTracker listens on the same local port Dota's Game State Integration
# posts to, and this feeds it the same shape of JSON, so the live view and
# the overlay behave as they would in a real game. Every payload also carries
# `thetracker_simulated`, which real Dota never sends: the app shows the
# match but never saves it, so test runs stay out of your match history.
#
# The overlay shows a reminder only in the five seconds before an event.
# The clock starts at 3:20 and runs forward, so the first minute passes a
# stack pull (3:53) and the 4:00 bounty and water runes.
#
#   Run:   powershell -ExecutionPolicy Bypass -File test-overlay.ps1
#   Stop:  Ctrl+C  (the match is then ended cleanly so the overlay hides)

param(
    [int]$StartClock = 200,   # seconds into the match to begin
    [int]$Seconds    = 180    # how long to run for
)

$uri = "http://127.0.0.1:3000/"
$matchId = "999" + (Get-Random -Minimum 100000 -Maximum 999999)

function Send-State {
    param([int]$Clock, [string]$GameState, [bool]$Alive = $true, [int]$Gold = 2400)

    $body = @{
        # Tells TheTracker this is a test run, so the match is never saved.
        thetracker_simulated = $true
        map = @{
            matchid    = $matchId
            clock_time = $Clock
            game_state = $GameState
            daytime    = ([math]::Floor($Clock / 300) % 2) -eq 0
        }
        player = @{
            activity  = "playing"
            kills     = 6
            last_hits = 90 + [math]::Floor($Clock / 8)
            denies    = 7
            gold      = $Gold
        }
        hero  = @{ name = "npc_dota_hero_juggernaut"; alive = $Alive }
        items = @{ slot0 = @{ name = "item_power_treads" }; slot1 = @{ name = "item_mage_slayer" } }
    } | ConvertTo-Json -Depth 6 -Compress

    try {
        Invoke-WebRequest -Uri $uri -Method POST -Body $body -ContentType "application/json" `
            -TimeoutSec 4 -UseBasicParsing | Out-Null
        return $true
    } catch {
        return $false
    }
}

Write-Host ""
Write-Host "  Simulating a Dota match against TheTracker" -ForegroundColor Cyan
Write-Host "  match id $matchId, starting at $([math]::Floor($StartClock/60)):$('{0:d2}' -f ($StartClock%60))"
Write-Host ""

if (-not (Send-State -Clock $StartClock -GameState "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS")) {
    Write-Host "  Could not reach TheTracker on port 3000." -ForegroundColor Red
    Write-Host "  Start the app first, then run this again." -ForegroundColor Red
    Write-Host ""
    exit 1
}

Write-Host "  Connected. This test match is not saved to your history." -ForegroundColor Green
Write-Host "  With the overlay open, a countdown shows 5 seconds before each event:" -ForegroundColor DarkGray
Write-Host "    :53 each minute  stack pull" -ForegroundColor DarkGray
Write-Host "    4:00, 8:00       bounty runes (water rune at 4:00 too)" -ForegroundColor DarkGray
Write-Host "    6:00 onward      power runes every 2 min" -ForegroundColor DarkGray
Write-Host "    6:00, 9:00       healing lotuses" -ForegroundColor DarkGray
Write-Host "    7:00, 14:00      wisdom runes" -ForegroundColor DarkGray
Write-Host ""
Write-Host "  Ctrl+C to stop and end the match." -ForegroundColor DarkGray
Write-Host ""

$clock = $StartClock
try {
    for ($i = 0; $i -lt $Seconds; $i++) {
        # A death partway through, so the gold-lost chip has something to show.
        $alive = -not ($i -ge 20 -and $i -lt 26)
        $gold = if ($i -ge 20) { 1500 } else { 2400 }

        Send-State -Clock $clock -GameState "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS" -Alive $alive -Gold $gold | Out-Null

        $mm = [math]::Floor($clock / 60)
        $ss = '{0:d2}' -f ($clock % 60)
        Write-Host "`r  clock $mm`:$ss   " -NoNewline

        Start-Sleep -Seconds 1
        $clock++
    }
} finally {
    # Always end the match, even on Ctrl+C, so the overlay hides itself
    # rather than being left open over the desktop.
    Write-Host ""
    Write-Host "  Ending the match..." -ForegroundColor DarkGray
    Send-State -Clock $clock -GameState "DOTA_GAMERULES_STATE_POST_GAME" | Out-Null
    Start-Sleep -Seconds 3
    Write-Host "  Done. The overlay should have closed itself." -ForegroundColor Green
    Write-Host ""
}
