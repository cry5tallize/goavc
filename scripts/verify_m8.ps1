# M8开发期验收：编排现有Go测试、直接后端基准和固定主场景，不修改codec策略。
# 不支持Resume；目录存在即拒绝，避免把中断/旧配置产物视作通过。
param(
    [Parameter(Mandatory=$true)][string]$OutputDirectory,
    [ValidateSet('validation','performance')][string]$Phase = 'validation',
    [ValidateRange(300,600)][int]$Frames = 300,
    [ValidateRange(5,20)][int]$Iterations = 5
)
$ErrorActionPreference = 'Stop'
$oldCGO, $oldProcs, $oldArch, $oldOS = $env:CGO_ENABLED, $env:GOMAXPROCS, $env:GOARCH, $env:GOOS
$oldAMD64 = $env:GOAMD64
$oldConsoleEncoding, $oldPipeEncoding = [Console]::OutputEncoding, $OutputEncoding
$oldInput, $oldFrames, $oldHash, $oldLong = $env:GOAVC_M8_INPUT, $env:GOAVC_M8_FRAMES, $env:GOAVC_M8_YUV_SHA256, $env:H264_LONG_TEST
if (Test-Path -LiteralPath $OutputDirectory) { throw '输出目录已存在；请使用新的目录，不支持Resume' }
New-Item -ItemType Directory -Path $OutputDirectory | Out-Null
$dir = (Resolve-Path $OutputDirectory).Path
$checks = @()

function Assert-Exit([string]$Description) {
    if ($LASTEXITCODE -ne 0) { throw "$Description 失败，退出码$LASTEXITCODE" }
}
function Run-Go([string]$Name,[string[]]$GoArgs) {
    $ErrorActionPreference = 'Continue'
    $path = Join-Path $dir "$Name.txt"
    & go @GoArgs *> $path
    Assert-Exit $Name
    $script:checks += [pscustomobject]@{ Name=$Name; Command=('go '+($GoArgs -join ' ')); SHA256=(Get-FileHash $path -Algorithm SHA256).Hash }
}
function Run-FFmpeg([string[]]$Arguments) {
    # Windows PowerShell会将正常原生stderr包装为ErrorRecord；按退出码判定。
    $ErrorActionPreference = 'Continue'
    & ffmpeg @Arguments 2>> (Join-Path $dir 'ffmpeg.log')
    Assert-Exit 'FFmpeg'
}
function Run-Benchmark([string]$Name,[string]$Binary,[string]$Filter,[string]$BenchTime) {
    $ErrorActionPreference = 'Continue'
    $path=Join-Path $dir "$Name.txt"
    $testArgs=@('-test.run','^$','-test.bench',$Filter,'-test.benchtime',$BenchTime,'-test.count',"$Iterations",'-test.benchmem')
    & $Binary @testArgs *> $path
    Assert-Exit $Name
    if (@(Select-String -LiteralPath $path -Pattern '^Benchmark\S+\s+\d+').Count -eq 0) { throw "$Name 未实际运行基准" }
}
function Decoder-Sequence([string]$Name,[string]$Path,[string]$Hash,[int]$Count,[string]$Binary) {
    $ErrorActionPreference = 'Continue'
    $env:GOAVC_M8_INPUT=$Path
    $env:GOAVC_M8_FRAMES=[string]$Count
    $env:GOAVC_M8_YUV_SHA256=$Hash
    Run-Benchmark $Name $Binary '^BenchmarkDecoder_Sequence$' '1x'
}

try {
    # Windows PowerShell默认用系统代码页解码原生stdout，会破坏Go的UTF-8 JSON。
    [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
    $OutputEncoding = [Console]::OutputEncoding
    $env:CGO_ENABLED = '0'
    $env:GOARCH = 'amd64'
    $env:GOOS = 'windows'
    $env:GOMAXPROCS = '16'
    $env:GOAMD64 = 'v1'
    foreach ($tool in @('go','ffmpeg','ffprobe')) {
        if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "缺少工具$tool" }
    }
    $revision = (git rev-parse HEAD).Trim()
    Assert-Exit '源码版本'
    $goVersion = (& go version) -join ' '
    Assert-Exit 'Go版本'
    $cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
    $metadata = [ordered]@{ Phase=$Phase; SourceRevision=$revision; Go=$goVersion; Host='windows/amd64'; CGO=0; GOAMD64='v1'; GOMAXPROCS=16; CPU=$cpu.Name; Frames=$Frames; Iterations=$Iterations; Threads=1; ExternalLoadMonitored=$false; ResumeSupported=$false }
    $metadata | ConvertTo-Json -Depth 5 | Out-File (Join-Path $dir 'environment.json') -Encoding utf8

    if ($Phase -eq 'validation') {
        $format = @(gofmt -l .)
        if ($LASTEXITCODE -ne 0 -or $format.Count -gt 0) { throw "gofmt失败：$format" }
        Run-Go 'vet' @('vet','./...')
        Run-Go 'build' @('build','./...')
        $env:H264_LONG_TEST='1'
        Run-Go 'native-long' @('test','-count=1','-timeout=0','-json','./...')
        $env:H264_LONG_TEST='0'
        Run-Go 'purego-all' @('test','-tags','purego','-count=1','-json','./...')
        $env:GOARCH='386'
        Run-Go '386-all' @('test','-count=1','-json','./...')
        $env:GOARCH='amd64'
        Run-Go 'tagged-all' @('test','-tags','goavc_simdtest','-count=1','-json','./...')
        Run-Go 'deps' @('run','./tools/depscheck')
        Run-Go 'manifest' @('run','./tools/manifest','-file','testdata/manifest.json')
        foreach ($osName in @('windows','linux','darwin')) {
            foreach ($arch in @('amd64','arm64')) {
                $env:GOOS=$osName; $env:GOARCH=$arch
                Run-Go "build-$osName-$arch" @('build','./...')
                Run-Go "asmvet-$osName-$arch" @('vet','-tags','goavc_simdtest','./internal/simd')
                Run-Go "asmtest-$osName-$arch" @('test','-tags','goavc_simdtest','-c','-o',(Join-Path $dir "simd-$osName-$arch.test"),'./internal/simd')
            }
        }
        $env:GOOS='windows'; $env:GOARCH='amd64'
        $env:CGO_ENABLED='1'; $env:GOMAXPROCS='2'
        Run-Go 'race-all' @('test','-race','-count=1','-timeout','45m','-json','./...')
        Run-Go 'race-purego' @('test','-race','-tags','purego','-count=1','-json','./decoder','./encoder','./stream','./tools/encodebench')
        $env:CGO_ENABLED='0'; $env:GOMAXPROCS='16'
        Run-Go 'fuzz-cabac' @('test','-run','^$','-fuzz','^FuzzDecoder_Residual$','-fuzztime=5s','-parallel=2','./internal/entropy/cabac')
        Run-Go 'fuzz-b' @('test','-run','^$','-fuzz','^FuzzEncoder_BReorderRoundTrip$','-fuzztime=5s','-parallel=2','./tests/codec')
        # 已有解析像素、同步背压与取消回收；阈值和输入不因M8缓存而放宽。
        Run-Go 'stream-retention' @('run','./tools/streamcheck','-duration','30s','-warmup','5s','-sample-interval','5s')
        Run-Go 'compile-cli-decoder' @('build','-o',(Join-Path $dir 'h264dec.exe'),'./cmd/h264dec')
        Run-Go 'compile-cli-encoder' @('build','-o',(Join-Path $dir 'h264enc.exe'),'./cmd/h264enc')
        $manifest=Get-Content -LiteralPath 'testdata/manifest.json' -Raw -Encoding utf8 | ConvertFrom-Json
        $cliResults=@()
        foreach ($entry in $manifest.entries) {
            $raw=Join-Path $dir ($entry.name+'.yuv')
            & (Join-Path $dir 'h264dec.exe') -input $entry.path -output $raw -pixel-format $entry.pixFmt -threads 1 -progress 0 2>> (Join-Path $dir 'cli-decode.log')
            Assert-Exit "实际CLI $($entry.name)"
            $actual=(Get-FileHash $raw -Algorithm SHA256).Hash
            if ($actual -ne $entry.expectedYUVSha256) { throw "$($entry.name)原生像素SHA不符" }
            $cliResults += [pscustomobject]@{ Name=$entry.name; Bytes=(Get-Item $raw).Length; SHA256=$actual; Oracle=$entry.oracle }
            Remove-Item -LiteralPath $raw
        }
        $cliResults | ConvertTo-Json -Depth 5 | Out-File (Join-Path $dir 'cli-corpus.json') -Encoding utf8
        foreach ($pixel in @('yuv420p','yuv422p','yuv444p','yuv420p10le','yuv422p10le','yuv444p10le')) {
            $levels=256
            if ($pixel -like '*10le') { $levels=1024 }
            $raw=Join-Path $dir "$pixel-input.yuv"
            $filter="nullsrc=size=32x32:rate=30,format=$pixel,geq=lum='mod(X*3+Y*5+N*7,$levels)':cb='mod(X*7+Y*3+N*11,$levels)':cr='mod(X*11+Y*7+N*13,$levels)'"
            Run-FFmpeg @('-v','warning','-y','-f','lavfi','-i',$filter,'-frames:v','6','-pix_fmt',$pixel,'-f','rawvideo',$raw)
            foreach ($rate in @('cqp','abr','cbr','vbv')) {
                $name="cli-$pixel-$rate"
                $coded=Join-Path $dir "$name.264"
                $decoded=Join-Path $dir "$name.yuv"
                $oracle=Join-Path $dir "$name-oracle.yuv"
                & (Join-Path $dir 'h264enc.exe') -input $raw -output $coded -width 32 -height 32 -pixel-format $pixel -profile high444 -entropy cabac -bframes 2 -gop 6 -idr 6 -slices 2 -lookahead 2 -8x8 -weighted -rate $rate -bitrate 60000 -vbv-buffer-size 60000 -threads 1 -progress 0 2>> (Join-Path $dir 'cli-encode.log')
                Assert-Exit $name
                & (Join-Path $dir 'h264dec.exe') -input $coded -output $decoded -pixel-format $pixel -threads 1 -progress 0 2>> (Join-Path $dir 'cli-decode.log')
                Assert-Exit "$name decode"
                Run-FFmpeg @('-v','warning','-xerror','-y','-i',$coded,'-fps_mode','passthrough','-pix_fmt',$pixel,'-f','rawvideo',$oracle)
                $expected=(Get-FileHash $oracle -Algorithm SHA256).Hash
                if ((Get-FileHash $decoded -Algorithm SHA256).Hash -ne $expected) { throw "$name CLI与FFmpeg像素不符" }
                $frameBytes=1536
                if ($pixel -like 'yuv422*') { $frameBytes=2048 }
                if ($pixel -like 'yuv444*') { $frameBytes=3072 }
                if ($pixel -like '*10le') { $frameBytes*=2 }
                if ((Get-Item $decoded).Length -ne $frameBytes*6) { throw "$name 排空帧数不符" }
                [pscustomobject]@{ Name=$name; InputSHA256=(Get-FileHash $raw -Algorithm SHA256).Hash; StreamSHA256=(Get-FileHash $coded -Algorithm SHA256).Hash; YUVSHA256=$expected; Frames=6 } | ConvertTo-Json | Out-File (Join-Path $dir "$name.json") -Encoding utf8
                Remove-Item -LiteralPath $decoded,$oracle
            }
        }
    } else {
        # 全部构建先完成；测量期间不再启动Go编译、Oracle或其他长测。
        Run-Go 'compile-simd' @('test','-c','-o',(Join-Path $dir 'simd.test.exe'),'./internal/simd')
        Run-Go 'compile-prediction' @('test','-c','-o',(Join-Path $dir 'prediction.test.exe'),'./internal/prediction')
        Run-Go 'compile-decoder-auto' @('test','-c','-o',(Join-Path $dir 'decoder-auto.test.exe'),'./decoder')
        Run-Go 'compile-decoder-generic' @('test','-tags','purego','-c','-o',(Join-Path $dir 'decoder-generic.test.exe'),'./decoder')
        Run-Go 'compile-encoder-control' @('test','-c','-o',(Join-Path $dir 'encoder-control.test.exe'),'./encoder')
        Run-Go 'compile-encoder-auto' @('build','-o',(Join-Path $dir 'encodebench-auto.exe'),'./tools/encodebench')
        Run-Go 'compile-encoder-generic' @('build','-tags','purego','-o',(Join-Path $dir 'encodebench-generic.exe'),'./tools/encodebench')
        Run-Benchmark 'kernels' (Join-Path $dir 'simd.test.exe') '.' '100ms'
        Run-Benchmark 'prediction' (Join-Path $dir 'prediction.test.exe') '.' '100ms'
        Run-Benchmark 'encoder-controls-auto' (Join-Path $dir 'encoder-control.test.exe') '^BenchmarkEncoder(P|B)_720p_Generic$' '1x'
        Run-Benchmark 'decoder-control-auto' (Join-Path $dir 'decoder-auto.test.exe') '^BenchmarkDecoder_1080p_Generic$' '1x'
        foreach ($profile in @('baseline','high')) {
            foreach ($slices in @(1,4)) {
                $name="decode-1080p-$profile-s$slices"
                $path=Join-Path $dir "$name.264"
                $params="keyint=60:min-keyint=60:scenecut=0:ref=2:threads=1:slices=$slices"
                $bframes=2
                if ($profile -eq 'baseline') { $params+=':cabac=0:weightp=0:8x8dct=0'; $bframes=0 }
                else { $params+=':cabac=1:b-adapt=0:b-pyramid=none:weightp=1:8x8dct=1' }
                Run-FFmpeg @('-v','warning','-y','-f','lavfi','-i','testsrc2=size=1920x1080:rate=30','-frames:v',"$Frames",'-pix_fmt','yuv420p','-c:v','libx264','-preset','veryfast','-profile:v',$profile,'-qp','26','-bf',"$bframes",'-x264-params',$params,'-f','h264',$path)
                $raw=Join-Path $dir "$name.yuv"
                Run-FFmpeg @('-v','warning','-xerror','-y','-i',$path,'-fps_mode','passthrough','-pix_fmt','yuv420p','-f','rawvideo',$raw)
                if ((Get-Item $raw).Length -ne [int64]1920*1080*3/2*$Frames) { throw '独立解码帧数/尺寸不符' }
                $hash=(Get-FileHash $raw -Algorithm SHA256).Hash
                foreach ($backend in @('generic','auto')) {
                    Decoder-Sequence "$name-$backend" $path $hash $Frames (Join-Path $dir "decoder-$backend.test.exe")
                }
                Remove-Item -LiteralPath $raw
                [pscustomobject]@{ Name=$name; InputSHA256=(Get-FileHash $path -Algorithm SHA256).Hash; ExpectedYUVSHA256=$hash; Frames=$Frames; SliceCount=$slices; Profile=$profile } | ConvertTo-Json | Out-File (Join-Path $dir "$name-input.json") -Encoding utf8
            }
        }
        $inputPath=Join-Path $dir 'encode-720p-input.yuv'
        Run-FFmpeg @('-v','warning','-y','-f','lavfi','-i','testsrc2=size=1280x720:rate=30','-frames:v',"$Frames",'-pix_fmt','yuv420p','-f','rawvideo',$inputPath)
        foreach ($backend in @('generic','auto')) {
            foreach ($iteration in 1..$Iterations) {
                $name="encode-720p-$backend-$iteration"
                $pending=Join-Path $dir "$name.pending"
                $stream=Join-Path $dir "$name.264"
                & (Join-Path $dir "encodebench-$backend.exe") -measure -threads 1 -input $inputPath -width 1280 -height 720 -frames $Frames -profile high -bframes 2 -gop 60 -idr 60 -qp 26 -transform8 -weighted -lookahead 2 -slices 4 -output $stream > $pending
                Assert-Exit $name
                Move-Item -LiteralPath $pending -Destination (Join-Path $dir "$name.json")
            }
        }
        # 所有吞吐样本完成后才执行独立像素Oracle；逐轮流SHA必须相同。
        $streams=@(Get-ChildItem -LiteralPath $dir -Filter 'encode-720p-*.264')
        $hashes=@($streams | ForEach-Object { (Get-FileHash $_.FullName -Algorithm SHA256).Hash } | Select-Object -Unique)
        if ($hashes.Count -ne 1) { throw 'Generic/Auto或重复轮次码流不一致' }
        $oracle=Join-Path $dir 'encode-720p-oracle.yuv'
        Run-FFmpeg @('-v','warning','-xerror','-y','-i',$streams[0].FullName,'-fps_mode','passthrough','-pix_fmt','yuv420p','-f','rawvideo',$oracle)
        $oracleHash=(Get-FileHash $oracle -Algorithm SHA256).Hash
        foreach ($backend in @('generic','auto')) {
            Decoder-Sequence "encode-720p-reconstruction-$backend" $streams[0].FullName $oracleHash $Frames (Join-Path $dir "decoder-$backend.test.exe")
        }
        Remove-Item -LiteralPath $oracle
        # 扩展是六帧诊断，不替代300帧主场景；4K不声称实时。
        foreach ($size in @(@(320,240),@(640,480),@(1920,1080),@(3840,2160))) {
            $w,$h=$size
            $raw=Join-Path $dir "diagnostic-$($w)x$($h).yuv"
            Run-FFmpeg @('-v','warning','-y','-f','lavfi','-i',"testsrc2=size=$($w)x$($h):rate=30",'-frames:v','6','-pix_fmt','yuv420p','-f','rawvideo',$raw)
            & (Join-Path $dir 'encodebench-auto.exe') -measure -threads 1 -input $raw -width $w -height $h -frames 6 -profile high -bframes 2 -gop 60 -idr 60 -qp 26 -transform8 -weighted -lookahead 2 -slices 4 > (Join-Path $dir "diagnostic-$($w)x$($h).json")
            Assert-Exit '扩展分辨率诊断'
            Remove-Item -LiteralPath $raw
        }
        # CPU/分配profile独立运行，绝不将插桩样本计入主目标中位数。
        $profileInput=Join-Path $dir 'profile-input.yuv'
        Run-FFmpeg @('-v','warning','-y','-f','lavfi','-i','testsrc2=size=1280x720:rate=30','-frames:v','30','-pix_fmt','yuv420p','-f','rawvideo',$profileInput)
        & (Join-Path $dir 'encodebench-auto.exe') -measure -threads 1 -input $profileInput -width 1280 -height 720 -frames 30 -profile high -bframes 2 -gop 60 -idr 60 -qp 26 -transform8 -weighted -lookahead 2 -slices 4 -cpu-profile (Join-Path $dir 'encoder.cpu') > (Join-Path $dir 'profile-encoder.json')
        Assert-Exit '独立编码热点采样'
        Run-Go 'encoder-cpu-top' @('tool','pprof','-top',(Join-Path $dir 'encodebench-auto.exe'),(Join-Path $dir 'encoder.cpu'))
        Remove-Item -LiteralPath $profileInput
    }
    [pscustomobject]@{ Completed=$true; Phase=$Phase; Checks=$checks; Scope='本机可执行测试；不代表未实施并行/MC插值SIMD或NEON原生验收' } | ConvertTo-Json -Depth 8 | Out-File (Join-Path $dir 'completed.json') -Encoding utf8
} finally {
    $env:CGO_ENABLED=$oldCGO; $env:GOMAXPROCS=$oldProcs; $env:GOARCH=$oldArch; $env:GOOS=$oldOS
    $env:GOAMD64=$oldAMD64
    [Console]::OutputEncoding=$oldConsoleEncoding; $OutputEncoding=$oldPipeEncoding
    $env:GOAVC_M8_INPUT=$oldInput; $env:GOAVC_M8_FRAMES=$oldFrames; $env:GOAVC_M8_YUV_SHA256=$oldHash; $env:H264_LONG_TEST=$oldLong
}
