$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../../..')).Path
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ('novelagent-release-' + [guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $tempRoot
$serverProcess = $null
$client = [Net.Http.HttpClient]::new()
$client.Timeout = [TimeSpan]::FromSeconds(5)
try {
    $binary = Join-Path $tempRoot 'novelagent.exe'
    & go build -o $binary $repoRoot
    if ($LASTEXITCODE -ne 0) { throw 'Build failed' }
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    $port = $listener.LocalEndpoint.Port
    $listener.Stop()
    $dataRoot = Join-Path $tempRoot 'data'
    $serverProcess = Start-Process -FilePath $binary -ArgumentList @('-addr', "127.0.0.1:$port", '-data', $dataRoot) -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $tempRoot 'stdout.txt') -RedirectStandardError (Join-Path $tempRoot 'stderr.txt')
    $baseURL = "http://127.0.0.1:$port"
    $ready = $false
    for ($i = 0; $i -lt 100; $i++) {
        try {
            $response = $client.GetAsync("$baseURL/").GetAwaiter().GetResult()
            $ready = [int]$response.StatusCode -eq 200
            $response.Dispose()
            if ($ready) { break }
        } catch { }
        Start-Sleep -Milliseconds 50
    }
    if (-not $ready) { throw 'Server did not become ready' }
    $observations = [Collections.Generic.List[object]]::new()
    foreach ($path in @('/', '/api/project', '/api/memory', '/api/memory/initialize/status')) {
        $response = $client.GetAsync("$baseURL$path").GetAwaiter().GetResult()
        if ([int]$response.StatusCode -ne 200) { throw "GET $path failed" }
        $observations.Add([ordered]@{method='GET'; path=$path; status=200})
        $response.Dispose()
    }
    function Get-DataSnapshot {
        @(Get-ChildItem -LiteralPath $dataRoot -Recurse -File | Sort-Object FullName | ForEach-Object {
            [ordered]@{path=[IO.Path]::GetRelativePath($dataRoot, $_.FullName); sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash}
        }) | ConvertTo-Json -Depth 5 -Compress
    }
    $baseline = Get-DataSnapshot
    $cases = @(
        @{body='{"reinitialize":true}'; status=400; code='reinit_not_supported'},
        @{body='{}'; status=409; code='knowledge_not_ready'},
        @{body='{"reinitialize":false}'; status=409; code='knowledge_not_ready'},
        @{body='{"force":true}'; status=400; code='invalid_request'},
        @{body='{} {}'; status=400; code='invalid_request'},
        @{body=(' ' * 4097); status=413; code='request_too_large'}
    )
    foreach ($case in $cases) {
        $content = [Net.Http.StringContent]::new($case.body, [Text.Encoding]::UTF8, 'application/json')
        $response = $client.PostAsync("$baseURL/api/memory/initialize", $content).GetAwaiter().GetResult()
        $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult() | ConvertFrom-Json
        if ([int]$response.StatusCode -ne $case.status -or $body.code -ne $case.code) { throw "Unexpected response for $($case.code)" }
        if ((Get-DataSnapshot) -cne $baseline) { throw "Stored data changed for $($case.code)" }
        $observations.Add([ordered]@{method='POST'; path='/api/memory/initialize'; status=[int]$response.StatusCode; code=$body.code; dataUnchanged=$true})
        $response.Dispose()
        $content.Dispose()
    }
    [ordered]@{status='PASS'; scope='real HTTP backend smoke, empty isolated store; no UI or model-quality claim'; observations=$observations; baseline=($baseline | ConvertFrom-Json)} | ConvertTo-Json -Depth 8
} finally {
    $client.Dispose()
    if ($serverProcess -and -not $serverProcess.HasExited) {
        Stop-Process -Id $serverProcess.Id
        $serverProcess.WaitForExit()
    }
    $resolvedTemp = [IO.Path]::GetFullPath($tempRoot)
    $expectedPrefix = Join-Path ([IO.Path]::GetTempPath()) 'novelagent-release-'
    if (-not $resolvedTemp.StartsWith($expectedPrefix, [StringComparison]::OrdinalIgnoreCase)) { throw 'Unexpected cleanup path' }
    Remove-Item -LiteralPath $resolvedTemp -Recurse -Force
}
