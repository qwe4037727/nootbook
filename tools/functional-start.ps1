$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$runRoot = Join-Path $repoRoot '.tmp_acceptance_data_functional_20261022'
$secureCredential = Read-Host 'Test credential (not saved)' -AsSecureString
$credential = [Net.NetworkCredential]::new('', $secureCredential).Password
if ([string]::IsNullOrWhiteSpace($credential)) { throw 'Missing credential on stdin' }
$env:OPENAI_API_KEY = $credential
$env:HTTPS_PROXY = 'http://127.0.0.1:7897'
$env:HTTP_PROXY = 'http://127.0.0.1:7897'
$process = Start-Process -FilePath (Join-Path $runRoot 'novelagent.exe') -ArgumentList @('-addr','127.0.0.1:18086','-data',(Join-Path $runRoot 'data')) -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runRoot 'stdout.txt') -RedirectStandardError (Join-Path $runRoot 'stderr.txt')
[ordered]@{pid=$process.Id;url='http://127.0.0.1:18086'} | ConvertTo-Json
