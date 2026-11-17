param([string]$Out = 'tmp/c00', [string]$ExtraInputs = '')
$ErrorActionPreference = 'Stop'
$env:CGO_ENABLED='0'; $env:GOAMD64='v1'; $env:GOMAXPROCS='16'
if (Test-Path -LiteralPath $Out) { throw "Choose a fresh output directory: $Out" }
New-Item -ItemType Directory -Path $Out | Out-Null
$Out=(Resolve-Path -LiteralPath $Out).Path
$inputs=@(foreach ($name in @('decode-1080p-baseline-s1','decode-1080p-baseline-s4','decode-1080p-high-s1','decode-1080p-high-s4','high10-1080p-s1','high10-1080p-s4')) {
    $dir=if($name.StartsWith('high10')){'tmp/b05'}else{'tmp/m8-closeout/endpoints-au'}
    $m=Get-Content -Raw "$dir/$name-input.json" | ConvertFrom-Json
    [pscustomobject]@{Name=$name;Path="$dir/$name.264";Frames=$m.Frames;InputSHA256=$m.InputSHA256;ExpectedYUVSHA256=$m.ExpectedYUVSHA256}
})
if ($ExtraInputs) { $extra=Get-Content -Raw $ExtraInputs | ConvertFrom-Json; foreach($m in $extra){$inputs+= $m} }
foreach($m in $inputs){ $null=Resolve-Path -LiteralPath $m.Path }
$revision=(git rev-parse HEAD).Trim()
foreach($backend in @('auto','generic')){
    $tags=@()
    if($backend -eq 'generic'){$tags+= '-tags=purego'}
    go test -c @tags -o "$Out/decoder-$backend.exe" ./benchmarks
    if($LASTEXITCODE -ne 0){throw "Build failed: $backend"}
}
$runs=@(foreach($m in $inputs){
    $env:GOAVC_M8_INPUT=(Resolve-Path -LiteralPath $m.Path).Path
    $env:GOAVC_M8_FRAMES=[string]$m.Frames; $env:GOAVC_M8_YUV_SHA256=$m.ExpectedYUVSHA256
    foreach($backend in @('auto','generic')){
        $lines=& "$Out/decoder-$backend.exe" '-test.run=^$' '-test.bench=^BenchmarkDecoder_Sequence$' '-test.benchtime=1x' '-test.count=5' '-test.benchmem' '-test.timeout=30m'
        $code=$LASTEXITCODE
        $lines | Set-Content -Encoding UTF8 "$Out/$($m.Name)-$backend.txt"
        if($code -ne 0 -or -not ($lines -cmatch '^(PASS)$') -or -not ($lines -match "sha256=$($m.InputSHA256.ToLowerInvariant()) ")){throw "Benchmark or input identity mismatch: $($m.Name)/$backend"}
        $samples=@(foreach($line in $lines){
            if($line -match '^BenchmarkDecoder_Sequence\S*\s+1\s+(\d+) ns/op\s+([\d.]+) frames/op\s+[\d.]+ frames/s\s+(\d+) B/op\s+(\d+) allocs/op'){
                if([int]$Matches[2] -ne $m.Frames){throw 'Frame count mismatch'}
                [ordered]@{ns_op=[long]$Matches[1];b_op=[long]$Matches[3];allocs_op=[long]$Matches[4]}
            }
        })
        if($samples.Count -ne 5){throw "Expected five samples: $($m.Name)/$backend"}
        [ordered]@{input=$m.Name;backend=$backend;samples=$samples}
        [Console]::WriteLine("$($m.Name)/$backend complete")
    }
})
$manifest=[ordered]@{revision=$revision;go=(go version);cpu=(Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name);os=[Environment]::OSVersion.VersionString;cgo=0;goamd64='v1';gomaxprocs=16;threads=1;protocol='Sequence; 1x/count5 same process per cell; FFmpeg pixel reference verified outside timer';inputs=$inputs;runs=$runs}
$manifest | ConvertTo-Json -Depth 8 | Set-Content -Encoding UTF8 "$Out/results.json"
