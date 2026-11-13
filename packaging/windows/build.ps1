$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
Set-Location $root
$version = (& python packaging/version.py app).Trim()
$numericVersion = (& python packaging/version.py numeric4).Trim()
$rmapiCommit = 'f295d5466978954edabaae4c730d50d03740cfac'
$build = Join-Path $root 'build\release\windows'
$dist = Join-Path $root 'dist\windows'
$rmapi = Join-Path $build 'rmapi'
$helperSource = Join-Path $build 'helper'
Remove-Item -Recurse -Force $build,$dist -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force $build,$dist,$helperSource | Out-Null

git clone --quiet --filter=blob:none https://github.com/FNStudios-NI/rmapi.git $rmapi
if ($LASTEXITCODE -ne 0) { throw 'rmapi clone failed' }
git -C $rmapi checkout --quiet $rmapiCommit
if ($LASTEXITCODE -ne 0) { throw 'rmapi checkout failed' }
Copy-Item cmd\setup\payload\rmapi_safe_api.go.txt (Join-Path $rmapi 'api\api.go') -Force
Push-Location $rmapi
try {
  go mod edit '-droprequire=github.com/golang-jwt/jwt'
  if ($LASTEXITCODE -ne 0) { throw 'rmapi vulnerable JWT dependency removal failed' }
} finally { Pop-Location }
Copy-Item helper\rmfolder\main.go,helper\rmfolder\go.mod,helper\rmfolder\go.sum $helperSource
Push-Location $helperSource
try {
  go mod edit '-replace=github.com/juruen/rmapi=../rmapi'
  if ($LASTEXITCODE -ne 0) { throw 'go mod edit failed' }
  go mod tidy
  if ($LASTEXITCODE -ne 0) { throw 'go mod tidy failed' }
} finally { Pop-Location }

$icon = Join-Path $build 'app-icon.ico'
& (Join-Path $PSScriptRoot 'build-icon.ps1') -Output $icon

foreach ($arch in @('amd64','arm64')) {
  $binDir = Join-Path $build $arch
  New-Item -ItemType Directory -Force $binDir | Out-Null
  $env:CGO_ENABLED = '0'
  $env:GOOS = 'windows'
  $env:GOARCH = $arch
  go build -trimpath -ldflags='-s -w' -o (Join-Path $binDir 'iserv-remarkable-setup.exe') ./cmd/setup
  if ($LASTEXITCODE -ne 0) { throw "setup build failed for $arch" }
  Push-Location $helperSource
  try {
    go build -trimpath -ldflags='-s -w' -o (Join-Path $binDir 'iserv-rm-folder-helper.exe') .
    if ($LASTEXITCODE -ne 0) { throw "helper build failed for $arch" }
  } finally { Pop-Location }
  Copy-Item $icon (Join-Path $binDir 'app-icon.ico')
  Copy-Item README.md,LICENSE,THIRD_PARTY_NOTICES.md,SECURITY.md,PRIVACY.md $binDir
  foreach ($required in @('iserv-remarkable-setup.exe','iserv-rm-folder-helper.exe','app-icon.ico','THIRD_PARTY_NOTICES.md')) {
    $path = Join-Path $binDir $required
    if (-not (Test-Path $path) -or (Get-Item $path).Length -eq 0) { throw "missing $path" }
  }
}

$iscc = $null
$programFilesX86 = ${env:ProgramFiles(x86)}
$candidates = @(
  $(if ($programFilesX86) { Join-Path $programFilesX86 'Inno Setup 6\ISCC.exe' }),
  $(if ($env:ProgramFiles) { Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe' }),
  $(if ($programFilesX86) { Join-Path $programFilesX86 'Inno Setup 7\ISCC.exe' }),
  $(if ($env:ProgramFiles) { Join-Path $env:ProgramFiles 'Inno Setup 7\ISCC.exe' })
) | Where-Object { $_ }
foreach ($candidate in $candidates) { if (Test-Path $candidate) { $iscc = $candidate; break } }
if (-not $iscc) {
  $cmd = Get-Command ISCC.exe -ErrorAction SilentlyContinue
  if ($cmd) { $iscc = $cmd.Source }
}
if (-not $iscc) { throw 'Inno Setup compiler was not found on the runner.' }

$targets = @(
  @{ Arch='amd64'; Allowed='x64os'; Mode='x64os' },
  @{ Arch='arm64'; Allowed='arm64'; Mode='arm64' }
)
foreach ($target in $targets) {
  $arch = $target.Arch
  $binDir = Join-Path $build $arch
  & $iscc "/DArch=$arch" "/DArchAllowed=$($target.Allowed)" "/DArchInstallMode=$($target.Mode)" "/DVersion=$version" "/DNumericVersion=$numericVersion" "/DBinDir=$binDir" "/DOutputDir=$dist" "/DSetupIcon=$icon" packaging\windows\installer.iss
  if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed for $arch" }
  $installer = Join-Path $dist "IServ-reMarkable-windows-$arch-Setup.exe"
  if (-not (Test-Path $installer) -or (Get-Item $installer).Length -lt 100000) { throw "installer missing or unexpectedly small: $installer" }
  $hash = (Get-FileHash -Algorithm SHA256 $installer).Hash.ToLowerInvariant()
  $checksumLine = "$hash  $([IO.Path]::GetFileName($installer))`n"
  [IO.File]::WriteAllText("$installer.sha256", $checksumLine, [Text.UTF8Encoding]::new($false))
}

# A native x64 runner can execute the x64 installer. Override the destination so
# the smoke test is independent of remembered Inno Setup paths on runner images.
$x64Installer = Join-Path $dist 'IServ-reMarkable-windows-amd64-Setup.exe'
$smokeBase = if ($env:RUNNER_TEMP) { $env:RUNNER_TEMP } else { [IO.Path]::GetTempPath() }
$smokeDir = Join-Path $smokeBase 'iserv-remarkable-installer-smoke'
$smokeLog = Join-Path $smokeBase 'iserv-remarkable-installer-smoke.log'
Remove-Item -Recurse -Force $smokeDir -ErrorAction SilentlyContinue
Remove-Item -Force $smokeLog -ErrorAction SilentlyContinue
$installArgs = @(
  '/VERYSILENT',
  '/SUPPRESSMSGBOXES',
  '/NORESTART',
  '/SP-',
  "/DIR=`"$smokeDir`"",
  "/LOG=`"$smokeLog`""
)
$installProcess = Start-Process -FilePath $x64Installer -ArgumentList $installArgs -Wait -PassThru
if ($installProcess.ExitCode -ne 0) {
  if (Test-Path $smokeLog) { Get-Content $smokeLog }
  throw "x64 silent installer smoke test failed with exit code $($installProcess.ExitCode)"
}
$installedFiles = @(
  'iserv-remarkable-setup.exe',
  'iserv-rm-folder-helper.exe',
  'app-icon.ico',
  'THIRD_PARTY_NOTICES.md',
  'README.md',
  'SECURITY.md',
  'PRIVACY.md'
)
$missingInstalled = @($installedFiles | Where-Object { -not (Test-Path (Join-Path $smokeDir $_)) })
if ($missingInstalled.Count -ne 0) {
  if (Test-Path $smokeLog) { Get-Content $smokeLog }
  throw "x64 installer omitted required files: $($missingInstalled -join ', ')"
}
$helperSelfTest = & (Join-Path $smokeDir 'iserv-rm-folder-helper.exe') --self-test
if ($LASTEXITCODE -ne 0 -or $helperSelfTest -ne 'iserv-rm-folder-helper: ok') {
  throw 'installed x64 folder helper self-test failed'
}
$uninstaller = Join-Path $smokeDir 'unins000.exe'
if (-not (Test-Path $uninstaller)) { throw 'x64 installer did not install its uninstaller' }
$uninstallProcess = Start-Process -FilePath $uninstaller -ArgumentList @(
  '/VERYSILENT',
  '/SUPPRESSMSGBOXES',
  '/NORESTART'
) -Wait -PassThru
if ($uninstallProcess.ExitCode -ne 0) {
  throw "x64 silent uninstall smoke test failed with exit code $($uninstallProcess.ExitCode)"
}
$remainingFiles = @($installedFiles | Where-Object { Test-Path (Join-Path $smokeDir $_) })
if ($remainingFiles.Count -ne 0) { throw "x64 uninstaller left installed files: $($remainingFiles -join ', ')" }
Remove-Item -Recurse -Force $smokeDir -ErrorAction SilentlyContinue
Remove-Item -Force $smokeLog -ErrorAction SilentlyContinue

Set-Content -Path (Join-Path $dist 'IServ-reMarkable-windows.version.txt') -Value $version -Encoding ascii
Write-Host "Built Windows RC installers in $dist"
