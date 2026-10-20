<#
.SYNOPSIS
  Tries an Electricity Maps API key against a zone and says in plain words what it is allowed to read.

.DESCRIPTION
  Asks for the key with the typing hidden, so it never goes in the command line or this file's history. The key is given to curl through
  its standard input rather than as an argument, and is blanked out of anything printed.

  KVGuzzler needs the first one (carbon intensity, latest). The others only show what the key does cover, to find out why it is refused.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\test-carbon-key.ps1
  powershell -ExecutionPolicy Bypass -File scripts\test-carbon-key.ps1 -Zone US-MIDW-MISO
#>
param([string]$Zone = 'US-MIDW-MISO')

$ErrorActionPreference = 'Stop'

$secure = Read-Host 'Paste your Electricity Maps API key (typing is hidden), then press Enter' -AsSecureString
$bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
try { $key = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr).Trim() }
finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr) }
if (-not $key) { Write-Host 'No key entered.'; exit 1 }
Write-Host ("Key length: {0} characters. Zone: {1}" -f $key.Length, $Zone)

function Invoke-Try([string]$Label, [string]$Url) {
    Write-Host ''
    Write-Host "== $Label" -ForegroundColor Cyan
    Write-Host $Url
    # The key goes in through curl's config on stdin (not the command line).
    $config = 'header = "auth-token: ' + $key.Replace('\', '\\').Replace('"', '\"') + '"'
    $raw = $config | & curl.exe -sS -m 20 -K - -w "`n%{http_code}" $Url 2>&1 | Out-String
    $raw = $raw.Replace($key, '(key)').TrimEnd()
    $lines = $raw -split "`n"
    $code = $lines[-1].Trim()
    $body = ($lines[0..([Math]::Max(0, $lines.Length - 2))] -join "`n").Trim()
    Write-Host "HTTP $code"
    if ($body.Length -gt 400) { $body = $body.Substring(0, 400) + '...' }
    if ($body) { Write-Host $body }

    if ($code -match '^2') {
        Write-Host 'ALLOWED: this key can read this.' -ForegroundColor Green
    } elseif ($body -match 'do not have access') {
        Write-Host 'NOT ALLOWED: the key is valid, but it is not set up for this data for this zone.' -ForegroundColor Yellow
    } elseif ($code -eq '401' -or $code -eq '403') {
        Write-Host 'REFUSED: the key itself was not accepted. Check it was copied whole.' -ForegroundColor Red
    } elseif ($code -eq '404') {
        Write-Host 'NOT FOUND: Electricity Maps does not know this zone code.' -ForegroundColor Red
    } elseif ($code -eq '429') {
        Write-Host 'SLOW DOWN: too many requests. Wait a while and try again.' -ForegroundColor Yellow
    } else {
        Write-Host 'No usable answer (000 means curl could not connect: check the internet connection).' -ForegroundColor Yellow
    }
}

$z = [Uri]::EscapeDataString($Zone)
$base = 'https://api.electricitymaps.com/v4'
Invoke-Try 'carbon intensity, latest (what KVGuzzler needs)' "$base/carbon-intensity/latest?zone=$z"
Invoke-Try 'carbon intensity, last 24 hours' "$base/carbon-intensity/history?zone=$z"
Invoke-Try 'power breakdown, latest' "$base/power-breakdown/latest?zone=$z"
Invoke-Try 'electricity price, day ahead (the portal example)' "$base/price-day-ahead/latest?zone=$z"

$key = $null
Write-Host ''
Write-Host 'Done. Nothing was saved anywhere.'
