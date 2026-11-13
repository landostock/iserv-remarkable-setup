param(
  [Parameter(Mandatory=$true)][string]$Output
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
$size = 256
$bitmap = New-Object System.Drawing.Bitmap $size, $size
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
$graphics.Clear([System.Drawing.Color]::FromArgb(23,107,91))

$white = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(248,252,251))
$soft = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(220,237,234))
$accent = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(35,139,118))
$pen = New-Object System.Drawing.Pen ([System.Drawing.Color]::White), 12
$pen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
$pen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round

$graphics.FillRectangle($white, 42, 63, 75, 104)
$graphics.FillRectangle($soft, 154, 51, 66, 126)
$graphics.FillRectangle($accent, 57, 86, 45, 7)
$graphics.FillRectangle($accent, 57, 107, 36, 6)
$graphics.DrawLine($pen, 105, 128, 169, 128)
$points = [System.Drawing.Point[]]@(
  (New-Object System.Drawing.Point 169,128),
  (New-Object System.Drawing.Point 146,108),
  (New-Object System.Drawing.Point 146,148)
)
$graphics.FillPolygon((New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::White)), $points)

$directory = Split-Path -Parent $Output
if ($directory) { New-Item -ItemType Directory -Force $directory | Out-Null }
$icon = [System.Drawing.Icon]::FromHandle($bitmap.GetHicon())
$stream = [System.IO.File]::Open($Output, [System.IO.FileMode]::Create)
try { $icon.Save($stream) } finally { $stream.Dispose(); $icon.Dispose(); $graphics.Dispose(); $bitmap.Dispose() }
if (-not (Test-Path $Output) -or (Get-Item $Output).Length -lt 1000) { throw 'Generated Windows icon is missing or unexpectedly small.' }
