param([string]$Out='tmp/c00-inputs')
$ErrorActionPreference='Stop'
$version=& ffmpeg -version | Select-Object -First 1
if($LASTEXITCODE -ne 0){throw 'FFmpeg is required'}
if(Test-Path -LiteralPath $Out){throw "Choose a fresh input directory: $Out"}
New-Item -ItemType Directory -Path $Out | Out-Null
$cases=@(
    @{Name='desktop-static';Source='smptebars=size=1920x1080:rate=30';Filter='drawgrid=width=96:height=54:thickness=1:color=white';Depth=8;Slices=1;Rate=@('-qp','26')},
    @{Name='texture-noise';Source='testsrc2=size=1920x1080:rate=30';Filter='noise=alls=12:all_seed=31415:allf=t+u';Depth=10;Slices=4;Rate=@('-qp','26')},
    @{Name='lowrate-skip';Source='testsrc2=size=1920x1080:rate=30';Filter='fps=5,fps=30';Depth=8;Slices=4;Rate=@('-b:v','220k','-maxrate','220k','-bufsize','440k')}
)
$manifest=@(foreach($c in $cases){
    $name=$c.Name; $pix=if($c.Depth -eq 10){'yuv420p10le'}else{'yuv420p'}
    $profile=if($c.Depth -eq 10){'high10'}else{'high'}
    $path="$Out/$name.264"; $yuv="$Out/$name.yuv"
    $argsList=@('-v','error','-xerror','-f','lavfi','-i',$c.Source,'-vf',$c.Filter,'-frames:v','60','-pix_fmt',$pix,'-c:v','libx264','-preset','veryfast','-profile:v',$profile)+$c.Rate+@('-bf','2','-x264-params',"keyint=60:min-keyint=60:scenecut=0:ref=2:threads=1:slices=$($c.Slices):cabac=1:b-adapt=0:b-pyramid=none:8x8dct=1",'-f','h264',$path)
    & ffmpeg @argsList
    if($LASTEXITCODE -ne 0){throw "Generation failed: $name"}
    & ffmpeg -v error -xerror -i $path -fps_mode passthrough -pix_fmt $pix -f rawvideo $yuv
    $unit=if($c.Depth -eq 10){2}else{1}
    if($LASTEXITCODE -ne 0 -or (Get-Item $yuv).Length -ne (1920*1080*3/2*60*$unit)){throw "Oracle failed: $name"}
    [ordered]@{Name=$name;Path=$path;Frames=60;Depth=$c.Depth;Slices=$c.Slices;InputSHA256=(Get-FileHash -LiteralPath $path).Hash;ExpectedYUVSHA256=(Get-FileHash -LiteralPath $yuv).Hash;GeneratorArguments=$argsList;GeneratorVersion=$version}
})
$manifest | ConvertTo-Json -Depth 6 | Set-Content -Encoding UTF8 "$Out/inputs.json"
