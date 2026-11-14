# M5开发期长门禁；FFmpeg只作为Oracle，所有Go构建CGO=0。
# 结果写入显式输出目录；不改写仓库语料或降低conformance.md阈值。
param(
    [Parameter(Mandatory=$true)][string]$OutputDirectory,
    [ValidateSet('oracle','quality','rate','all')][string]$Mode = 'oracle',
    [int]$Frames = 300,
    [ValidateSet('720p','1080p')][string[]]$Sizes = @('720p','1080p'),
    [ValidateSet('abr','cbr','vbv')][string[]]$RateModes = @('abr','cbr','vbv'),
    [switch]$Resume
)
$ErrorActionPreference = 'Stop'
$env:CGO_ENABLED = '0'
if ($Frames -lt 300) { throw 'M5长门禁至少300帧' }
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
$dir = (Resolve-Path $OutputDirectory).Path
$exe = Join-Path $dir 'encodebench.exe'
go build -o $exe ./tools/encodebench
if ($LASTEXITCODE -ne 0) { throw '测量工具构建失败' }
$revision = (git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw '无法记录源码版本' }
foreach ($tool in @('ffmpeg','ffprobe')) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "缺少Oracle工具 $tool" }
}

function Assert-Exit([string]$Description) {
    if ($LASTEXITCODE -ne 0) { throw "$Description 失败，退出码$LASTEXITCODE" }
}

function Run-Go([string]$Name,[int]$Width,[int]$Height,[string]$InputPath,[int]$QP,[string]$RC,[int]$Rate,[bool]$Verify) {
    $report = Join-Path $dir "$Name.json"
    $stream = Join-Path $dir "$Name.264"
    $recon = Join-Path $dir "$Name.yuv"
    if ($Resume -and (Test-Path $report) -and (Test-Path $stream) -and (Test-Path $recon)) {
        try {
            $saved = Get-Content $report -Raw | ConvertFrom-Json
            if ($saved.Frames -eq $Frames -and $saved.Width -eq $Width -and $saved.Height -eq $Height -and
                $saved.QP -eq $QP -and $saved.RateControl -eq $RC -and $saved.Bytes -eq (Get-Item $stream).Length -and
                $saved.GOPSize -eq 60 -and $saved.IDRInterval -eq 60 -and $saved.MaxBFrames -eq 2 -and
                $saved.Bitrate -eq $Rate -and $saved.Profile -eq 'high' -and
                $saved.Revision -eq $revision -and -not $saved.Modified -and
                $saved.Transform8x8 -and $saved.WeightedPrediction -and $saved.Lookahead -eq 2 -and
                $saved.FPS -eq 30 -and $saved.FPSDen -eq 1 -and $saved.SliceCount -eq 4 -and
                $saved.FrameBits.Count -eq $Frames -and $saved.VBVFullnessSequence.Count -eq $Frames -and
                (-not $Verify -or $saved.DecoderVerified) -and
                $saved.InputSHA256 -eq (Get-FileHash $InputPath -Algorithm SHA256).Hash -and
                $saved.StreamSHA256 -eq (Get-FileHash $stream -Algorithm SHA256).Hash -and
                $saved.ReconstructionSHA256 -eq (Get-FileHash $recon -Algorithm SHA256).Hash -and
                (Get-Item $recon).Length -eq [int64]$Width*$Height*3/2*$Frames) {
                return @{ Report=$report; Stream=$stream; Recon=$recon }
            }
        } catch { Write-Warning "不完整产物重新生成：$Name" }
    }
    $argv = @('-width',$Width,'-height',$Height,'-frames',$Frames,'-profile','high','-bframes',2,
        '-gop',60,'-idr',60,'-qp',$QP,'-transform8','-weighted','-lookahead',2,'-slices',4,
        '-input',$InputPath,'-output',$stream,'-recon',$recon,'-rc',$RC,
        '-cpu-profile',(Join-Path $dir "$Name.pprof"))
    if ($Rate -gt 0) { $argv += @('-bitrate',$Rate,'-vbv-buffer',$Rate) }
    if ($Verify) { $argv += '-verify' }
    # 只有成功退出才发布报告；中断后的临时文件不能被Resume当作完成证据。
    $pending = "$report.pending"
    & $exe @argv | Out-File $pending -Encoding utf8
    Assert-Exit "Go编码$Name"
    Move-Item -LiteralPath $pending -Destination $report -Force
    return @{ Report=$report; Stream=$stream; Recon=$recon }
}

$qualityFailures = @()

foreach ($size in @(@(1280,720),@(1920,1080))) {
    $w,$h = $size
    $sizeName = '720p'
    if ($w -eq 1920) { $sizeName = '1080p' }
    if ($sizeName -notin $Sizes) { continue }
    $prefix = "high-$($w)x$($h)"
    $inputPath = Join-Path $dir "$prefix-input.yuv"
    ffmpeg -v warning -y -f lavfi -i "testsrc2=size=$($w)x$($h):rate=30" -frames:v $Frames -pix_fmt yuv420p -f rawvideo $inputPath
    Assert-Exit '生成参考YUV'
    if ($Mode -in @('oracle','all')) {
        $run = Run-Go "$prefix-oracle" $w $h $inputPath 26 'cqp' 0 $true
        $oracle = Join-Path $dir "$prefix-ffmpeg.yuv"
        ffmpeg -v warning -xerror -y -i $run.Stream -fps_mode passthrough -pix_fmt yuv420p -f rawvideo $oracle
        Assert-Exit 'FFmpeg独立解码'
        $localHash = (Get-FileHash $run.Recon -Algorithm SHA256).Hash
        $oracleHash = (Get-FileHash $oracle -Algorithm SHA256).Hash
        if ($localHash -ne $oracleHash) { throw "$prefix 像素Oracle不一致" }
        Write-Output "$prefix Oracle=$localHash"
    }
    if ($Mode -in @('quality','all')) {
        foreach ($qp in @(18,26,34)) {
            $name = "$prefix-qp$qp"
            $run = Run-Go "$name-go" $w $h $inputPath $qp 'cqp' 0 $true
            $oracle = Join-Path $dir "$name-ffmpeg.yuv"
            ffmpeg -v warning -xerror -y -i $run.Stream -fps_mode passthrough -pix_fmt yuv420p -f rawvideo $oracle
            Assert-Exit '质量序列独立像素Oracle'
            if ((Get-FileHash $oracle -Algorithm SHA256).Hash -ne (Get-FileHash $run.Recon -Algorithm SHA256).Hash) {
                throw "$name 独立Oracle像素不一致"
            }
            $x264 = Join-Path $dir "$name-x264.264"
            ffmpeg -v warning -y -f rawvideo -pixel_format yuv420p -video_size "$($w)x$($h)" -framerate 30 -i $inputPath -frames:v $Frames -c:v libx264 -preset veryfast -tune psnr -profile:v high -qp $qp -threads 1 -x264-params 'keyint=60:min-keyint=60:scenecut=0:bframes=2:b-adapt=0:b-pyramid=none:ref=2:open-gop=0:weightp=1:slices=4:ipratio=1.0:pbratio=1.0' -f h264 $x264
            Assert-Exit 'x264同QP参考编码'
            # Windows PowerShell将原生stderr包装为ErrorRecord；这里按真实退出码判定。
            $ErrorActionPreference = 'Continue'
            ffmpeg -v info -f rawvideo -pixel_format yuv420p -video_size "$($w)x$($h)" -framerate 30 -i $inputPath -i $x264 -lavfi psnr -f null NUL 2> (Join-Path $dir "$name-x264-psnr.txt")
            $ErrorActionPreference = 'Stop'
            Assert-Exit 'x264 PSNR测量'
            $ErrorActionPreference = 'Continue'
            ffmpeg -v info -f rawvideo -pixel_format yuv420p -video_size "$($w)x$($h)" -framerate 30 -i $inputPath -f rawvideo -pixel_format yuv420p -video_size "$($w)x$($h)" -framerate 30 -i $run.Recon -lavfi ssim -f null NUL 2> (Join-Path $dir "$name-go-ssim.txt")
            $ErrorActionPreference = 'Stop'
            Assert-Exit 'Go SSIM测量'
            $ErrorActionPreference = 'Continue'
            ffmpeg -v info -f rawvideo -pixel_format yuv420p -video_size "$($w)x$($h)" -framerate 30 -i $inputPath -i $x264 -lavfi ssim -f null NUL 2> (Join-Path $dir "$name-x264-ssim.txt")
            $ErrorActionPreference = 'Stop'
            Assert-Exit 'x264 SSIM测量'
            $metrics = Get-Content $run.Report -Raw | ConvertFrom-Json
            $psnrLog = Get-Content (Join-Path $dir "$name-x264-psnr.txt") -Raw
            $match = [regex]::Match($psnrLog,'PSNR y:([0-9.]+)')
            if (-not $match.Success) { throw "$name 缺少独立PSNR数值" }
            $x264Y = [double]::Parse($match.Groups[1].Value,[Globalization.CultureInfo]::InvariantCulture)
            $difference = [math]::Abs([double]$metrics.PSNR[0]-$x264Y)
            # 不在此中止整个矩阵：即使某项失败，也保留所有QP的非选择性报告。
            $gate = @{ Name=$name; GoY=$metrics.PSNR[0]; X264Y=$x264Y; AbsoluteDifference=$difference; Passed=($difference -le 1.5) }
            $gate | ConvertTo-Json | Out-File (Join-Path $dir "$name-quality-gate.json") -Encoding utf8
            if (-not $gate.Passed) { $qualityFailures += "$name Y-PSNR差$difference dB" }
            $target = [int][math]::Round($metrics.BitrateKbps * 1000)
            $matched = Join-Path $dir "$name-x264-matched.264"
            ffmpeg -v warning -y -f rawvideo -pixel_format yuv420p -video_size "$($w)x$($h)" -framerate 30 -i $inputPath -frames:v $Frames -c:v libx264 -preset veryfast -tune psnr -profile:v high -b:v $target -threads 1 -x264-params 'keyint=60:min-keyint=60:scenecut=0:bframes=2:b-adapt=0:b-pyramid=none:ref=2:open-gop=0:weightp=1:slices=4' -f h264 $matched
            Assert-Exit 'x264匹配目标码率编码'
            $requestedRate = $target
            $attempts = @()
            for ($attempt = 0; $attempt -lt 4; $attempt++) {
                $actualRate = (Get-Item $matched).Length * 8 * 30 / $Frames
                $attempts += @{ Requested=$requestedRate; Actual=$actualRate }
                if ([math]::Abs($actualRate-$target) -le $target*0.01) { break }
                $requestedRate = [int][math]::Round($requestedRate*$target/$actualRate)
                ffmpeg -v warning -y -f rawvideo -pixel_format yuv420p -video_size "$($w)x$($h)" -framerate 30 -i $inputPath -frames:v $Frames -c:v libx264 -preset veryfast -tune psnr -profile:v high -b:v $requestedRate -threads 1 -x264-params 'keyint=60:min-keyint=60:scenecut=0:bframes=2:b-adapt=0:b-pyramid=none:ref=2:open-gop=0:weightp=1:slices=4' -f h264 $matched
                Assert-Exit 'x264实测码率迭代匹配'
            }
            $ErrorActionPreference = 'Continue'
            ffmpeg -v info -f rawvideo -pixel_format yuv420p -video_size "$($w)x$($h)" -framerate 30 -i $inputPath -i $matched -lavfi psnr -f null NUL 2> (Join-Path $dir "$name-x264-matched-psnr.txt")
            $ErrorActionPreference = 'Stop'
            Assert-Exit 'x264匹配码率PSNR测量'
            $ErrorActionPreference = 'Continue'
            ffmpeg -v info -f rawvideo -pixel_format yuv420p -video_size "$($w)x$($h)" -framerate 30 -i $inputPath -i $matched -lavfi ssim -f null NUL 2> (Join-Path $dir "$name-x264-matched-ssim.txt")
            $ErrorActionPreference = 'Stop'
            Assert-Exit 'x264匹配码率SSIM测量'
            # 不自动把目标码率相同当作实测等码率；保留实际字节数供报告匹配误差。
            $actualMatched = (Get-Item $matched).Length * 8 * 30 / $Frames
            $rateError = 100 * ($actualMatched - $target) / $target
            $matchInfo = @{ TargetBitsPerSecond=$target; ActualBitsPerSecond=$actualMatched; ErrorPercent=$rateError; Attempts=$attempts; Matched=([math]::Abs($rateError) -le 1) }
            $matchInfo | ConvertTo-Json | Out-File (Join-Path $dir "$name-matched-rate.json") -Encoding utf8
            if (-not $matchInfo.Matched) { $qualityFailures += "$name 实测码率未匹配：$rateError%" }
            Write-Output "$name Go报告=$($run.Report) x264字节=$((Get-Item $x264).Length) 匹配目标字节=$((Get-Item $matched).Length) 实测匹配误差=$rateError%"
        }
    }
    if ($Mode -in @('rate','all')) {
        $rate = 2000000
        if ($w -eq 1920) { $rate = 4000000 }
        foreach ($rc in $RateModes) {
            $run = Run-Go "$prefix-$rc" $w $h $inputPath 26 $rc $rate $true
            $metrics = Get-Content $run.Report -Raw | ConvertFrom-Json
            $oracle = Join-Path $dir "$prefix-$rc-ffmpeg.yuv"
            ffmpeg -v warning -xerror -y -i $run.Stream -fps_mode passthrough -pix_fmt yuv420p -f rawvideo $oracle
            Assert-Exit '码控独立像素Oracle'
            if ((Get-FileHash $oracle -Algorithm SHA256).Hash -ne $metrics.ReconstructionSHA256) { throw "$prefix $rc 独立Oracle像素不一致" }
            if ($rc -eq 'abr' -and [math]::Abs($metrics.BitrateKbps*1000-$rate) -gt $rate*0.1) {
                throw "$prefix ABR超过±10%：$($metrics.BitrateKbps) kbit/s"
            }
            if ($rc -eq 'vbv' -and ($metrics.MinVBVFullness -lt 0 -or $metrics.MaxVBVFullness -gt $rate)) {
                throw "$prefix VBV缓冲越界"
            }
            if ($metrics.FrameBits.Count -ne $Frames) { throw "$prefix $rc 缺少逐AU比特记录" }
            if ($metrics.VBVFullnessSequence.Count -ne $Frames) { throw "$prefix $rc 缺少逐AU缓冲轨迹" }
            if ($rc -eq 'vbv') {
                # 独立重算充放水，不能只依赖生产代码报告的最小/最大值。
                [int64]$sumBits = 0
                for ($frame = 0; $frame -lt $Frames; $frame++) {
                    $sumBits += [int64]$metrics.FrameBits[$frame]
                    # 此脚本固定30FPS；非整除余数按累积总排空量处理。
                    $fullness = [int64]($metrics.VBVBufferBits/2) + $sumBits -
                        [int64][math]::Floor([double]$rate*($frame+1)/30)
                    if ($fullness -ne $metrics.VBVFullnessSequence[$frame] -or $fullness -lt 0 -or $fullness -gt $metrics.VBVBufferBits) {
                        throw "$prefix VBV第$frame 帧充放水不符或越界"
                    }
                }
            }
            $windows = @()
            for ($start = 0; $start -lt $Frames; $start += 30) {
                $end = [math]::Min($Frames,$start+30)
                [int64]$bits = 0
                for ($frame = $start; $frame -lt $end; $frame++) { $bits += $metrics.FrameBits[$frame] }
                $windows += @{ FirstPacket=$start; Packets=($end-$start); BitsPerSecond=($bits*30/($end-$start)) }
            }
            $windows | ConvertTo-Json | Out-File (Join-Path $dir "$prefix-$rc-windows.json") -Encoding utf8
            Write-Output "$prefix $rc $($metrics.BitrateKbps) kbit/s fullness=$($metrics.MinVBVFullness)..$($metrics.MaxVBVFullness)"
        }
    }
}
if ($qualityFailures.Count -gt 0) { throw ('M5质量门禁未通过；完整结果已保留：' + ($qualityFailures -join '; ')) }
