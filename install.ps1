# Install veyra on Windows.
#
#   From a downloaded zip:  extract it, then double-click install.cmd
#                           (or run: powershell -ExecutionPolicy Bypass -File install.ps1)
#     Installs the veyra.exe that sits next to this script; nothing is downloaded.
#   Without a zip:          irm https://raw.githubusercontent.com/RishiKendai/veyra/main/install.ps1 | iex
#     Downloads the latest release and verifies its checksum.
#
# Set $env:VEYRA_VERSION (e.g. v0.1.0) to pin a release.
$ErrorActionPreference = 'Stop'
$repo = 'RishiKendai/veyra'

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("veyra-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $local = if ($PSScriptRoot) { Join-Path $PSScriptRoot 'veyra.exe' } else { $null }
  if ($local -and (Test-Path $local)) {
    # Running from an extracted zip: install the binary next to this script.
    $src = $local
  } else {
    switch ($env:PROCESSOR_ARCHITECTURE) {
      'AMD64' { $arch = 'amd64' }
      'ARM64' { $arch = 'arm64' }
      default { throw "veyra: unsupported CPU $($env:PROCESSOR_ARCHITECTURE)" }
    }
    if ($env:VEYRA_VERSION) { $base = "https://github.com/$repo/releases/download/$($env:VEYRA_VERSION)" }
    else { $base = "https://github.com/$repo/releases/latest/download" }
    $archive = "veyra_windows_$arch.zip"

    Write-Host "Downloading $archive ..."
    Invoke-WebRequest "$base/$archive" -OutFile (Join-Path $tmp $archive)
    Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match " $([regex]::Escape($archive))$" } | Select-Object -First 1
    if (-not $line) { throw "veyra: no checksum listed for $archive" }
    $want = ($line -split '\s+')[0]
    $got = (Get-FileHash (Join-Path $tmp $archive) -Algorithm SHA256).Hash
    if ($want -ne $got) { throw 'veyra: checksum mismatch, refusing to install' }

    Expand-Archive (Join-Path $tmp $archive) -DestinationPath $tmp -Force
    $src = Join-Path $tmp 'veyra.exe'
  }

  $dir = Join-Path $env:LOCALAPPDATA 'veyra\bin'
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
  Copy-Item $src (Join-Path $dir 'veyra.exe') -Force
  Unblock-File (Join-Path $dir 'veyra.exe') -ErrorAction SilentlyContinue

  $path = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (($path -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', "$path;$dir", 'User')
    Write-Host "Added $dir to your PATH. Open a new terminal to use it."
  }
  Write-Host "Installed veyra to $dir\veyra.exe"
  if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
    Write-Host 'Note: scanning videos needs FFmpeg:  winget install Gyan.FFmpeg'
  }
  Write-Host 'Run:  veyra'
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
