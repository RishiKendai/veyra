# VEYRA

**V**isual **E**xploration & **Y**ield-based **R**ecognition **A**ssistant. Find duplicate and visually similar
**images and videos**, review the groups in a terminal UI, and open any file in its native app. Scanning is
strictly read-only: veyra never moves, renames or modifies your files, and writes nothing next to them. The only
way it removes anything is when *you* press `D` on a file and confirm with Enter: the file is then moved to the
Trash / Recycle Bin (recoverable).

## Install

**From a downloaded archive** (GitHub Releases): extract it and run the installer inside. It copies the
bundled binary to a folder on your `PATH`, so afterwards you just type `veyra` instead of `./veyra`.

```
# macOS / Linux
tar -xzf veyra_darwin_arm64.tar.gz      # or veyra_linux_amd64.tar.gz, etc.
sh install.sh                           # installs to /usr/local/bin, or ~/.local/bin if that is not writable

# Windows: extract veyra_windows_amd64.zip, then double-click install.cmd
# (or: powershell -ExecutionPolicy Bypass -File install.ps1). It installs to %LOCALAPPDATA%\veyra\bin
# and adds that folder to your PATH; open a new terminal afterwards.
```

**One command, no download by hand:**

```
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/RishiKendai/veyra/main/install.sh | sh

# Windows (PowerShell)
irm https://raw.githubusercontent.com/RishiKendai/veyra/main/install.ps1 | iex

# Homebrew (also installs FFmpeg)
brew install RishiKendai/tap/veyra

# From source (needs Go)
go install github.com/RishiKendai/veyra/cmd/veyra@latest
```

Installing never happens by itself: extracting an archive does not run anything, and no operating system lets
a downloaded archive execute code on its own. Running the installer (or the one-line command) is the single step.
Set `VEYRA_INSTALL_DIR` to choose another folder on macOS/Linux.

Binaries for macOS, Linux and Windows (amd64 and arm64) are attached to each GitHub release. They are not
code-signed, so a browser-downloaded binary may trigger a Gatekeeper / SmartScreen warning; the install
commands above avoid that on macOS. Scanning **videos** needs FFmpeg on your `PATH` (`brew install ffmpeg`,
`sudo apt install ffmpeg`, `winget install Gyan.FFmpeg`); veyra checks for it and prints the command for your OS.
Images need nothing else.

## Use

```
veyra                              # interactive menu: pick images/videos, recursive or not, then a folder
veyra -t image ~/Pictures          # files directly inside the folder
veyra -t image -r ~/Pictures       # recurse
veyra -t video -r /Volumes/Media
veyra -t image .                   # the current folder
veyra help                         # also: veyra -h, veyra version
```

Run with no arguments, veyra shows four choices (scan images, images recursively, videos, videos recursively),
then a folder browser (arrow keys to move, Enter to open a folder, `S` to scan the highlighted folder, `Tab`
to type or paste a path, `.` to show hidden folders). Without a terminal and without arguments it prints the
usage and exits with status 2.

On a terminal the results open in the TUI. When stdout is not a terminal (pipes, redirects) or there
are no duplicates, a plain-text report is printed instead. Progress and warnings go to stderr.
Exit status: 0 on success (including "no duplicates"), 2 for bad arguments, 1 for fatal errors, 130 if interrupted.

## The interface

Left: search, groups and files. Right: the selected file (with buttons) above its metadata.

| Key | Action |
|---|---|
| ↑ ↓ / j k, PgUp PgDn, Home End | Move |
| ← / → | Collapse group or go to parent / expand group or enter it |
| Space | Fold or unfold the current group |
| Enter | Open the selected file in its default app (on a group: fold) |
| O | Reveal in Finder / Explorer / your file manager |
| C | Copy the full path |
| D | Delete: asks "Move … to the Trash?"; **Enter** confirms, any other key cancels. Works on a file row only |
| [ ] / wheel over the metadata | Scroll the metadata when it does not fit |
| P | Quick Look (macOS only) |
| / | Fuzzy search; Enter keeps the filter, Esc clears it |
| ? | Help |
| Q / Ctrl+C | Quit |

Mouse: click selects (clicking a group header also folds it), the wheel scrolls, and the
**[ Preview ↵ ]**, **[ Reveal O ]**, **[ Copy path C ]** and the red **[ Delete D ]** buttons act on the selected file.
Hovering only highlights a row; it never changes what Enter or the buttons act on.
The UI needs at least 60×16 cells; smaller terminals get a "too small" screen.

## Platforms

Release binaries are built for macOS, Linux and Windows (amd64 and arm64). The native actions are chosen per OS:

| Action | macOS | Linux | Windows |
|---|---|---|---|
| Open (Enter) | `open` | `xdg-open` | `rundll32 url.dll,FileProtocolHandler` |
| Reveal (O) | `open -R` | file manager over D-Bus, else `xdg-open` on the folder | `explorer /select,` |
| Move to Trash (D) | Finder | `gio trash`, `trash-put` or `trash` | Recycle Bin via PowerShell |
| Copy path (C) | `pbcopy` | `wl-copy`, `xclip` or `xsel` | PowerShell `Set-Clipboard` |
| Quick Look (P) | `qlmanage -p` | not available | not available |

Only macOS has been run by hand. The Linux and Windows command lines are unit-tested, the program builds and
passes `go vet` for all six release targets, and CI runs the tests on macOS, Linux and Windows. Treat Linux and
Windows as new until you have confirmed them on a real machine. Known gaps there: external drives are only
recognised under `/Volumes/` (macOS), so a one-reader-at-a-time limit for hard disks is not applied elsewhere,
and the file creation time is shown as Unknown on Linux.

## What the numbers mean

- The header shows files examined, media files found, groups, matched files (every file in a group),
  duplicates, and, side by side, **how much storage the duplicates use** and the **total time** of the whole
  run (from program start until the results opened).
- In every group the **largest file is the original** (marked ★; ties keep the first path). All other files are
  duplicates, and their combined size is the storage figure. It is a size rule, not a quality judgement: in a
  VISUAL group the larger file is not necessarily the better one. The list marks originals with ★ and each
  group shows how much it frees; the FILE panel and the metadata do not repeat this.
- Metadata is never truncated: long names and paths wrap under their label, and if the panel is still too
  short you can scroll it.
- While scanning, one status line says what is happening and how far along it is (stage, "n of total", percent,
  elapsed time and an estimate of the time left).
- Deleting updates the list at once: a group left with one file disappears, the next-largest file becomes the
  original if the original was deleted, and the totals are recomputed. Delete needs macOS to let your terminal
  control Finder (System Settings → Privacy & Security → Automation); it asks the first time. Volumes mounted
  read-only cannot be deleted from.

## How matching works

1. **Discovery is content-based.** Extensions are never trusted. Images must decode; videos must
   contain a real video stream according to `ffprobe` (cover art and still images do not count).
   Corrupt or unsupported files are skipped with a reason on stderr.
2. **EXACT** – byte-identical. Files are narrowed by size (a unique size is never read), then by a
   BLAKE3 hash of three 4 KiB samples (head, middle, tail), and only then fully hashed with streaming
   BLAKE3 (2 MiB buffer per worker). Only the full hash decides a match. Hard links are ignored. Both
   hash stages are cached per path (validated by size and modification time) in the OS cache directory
   (`~/Library/Caches/veyra` on macOS; a cache left by the old name `r.dedup` is moved over); entries unused for 7 days are dropped. File metadata, image and
   video fingerprints are cached the same way, so a repeat scan of unchanged files does
   almost no work. A cache entry is trusted while the file's size and modification time are unchanged; a
   file replaced by different content with identical size and mtime would reuse a stale entry. Videos
   whose duration is not within 10% of any other video are never decoded, since they cannot match. The
   rest are probed with 6 middle frames first; only videos with a possible partner get the other 10
   (the probe is a necessary condition for a match, so it cannot hide one). Images are read one file at a
   time on external drives but decoded on all cores.
3. **VISUAL images** – a 32×32 luma thumbnail (after EXIF orientation) gives aHash, dHash and pHash.
   All three must be close (pHash ≤ 8, dHash ≤ 14, aHash ≤ 14 of 64 bits). Candidates are found with
   multi-index hashing instead of comparing every pair. Flat/blank images never match perceptually.
4. **VISUAL videos** (same content at a different quality, size or container): 16 frames at evenly
   spaced timestamps are decoded by one `ffmpeg` process and hashed. Two videos match when their
   durations are within 10%, each has ≥ 4 visually distinct shots, and ≥ 85% of at least 8 comparable
   frame pairs agree (small sample offsets tolerated). Only whole-video copies are matched: a clip cut
   out of a longer video, or a release with a different cut, is not detected.
5. **Grouping uses complete linkage**: every pair in a group must match, so A~B and B~C never pull in
   A when A and C do not match. Order and group numbers are deterministic. Groups carry warnings when
   durations or aspect ratios differ, since a crop, stretch or trim can still match.

Group "distance" values are raw matcher units, not percentages, and are not shown.

## Limits (v1)

- **Not detected:** mirrored or rotated copies (except by EXIF), heavy crops, overlays, and letterboxed
  video. Videos are only matched when they share the same cut: a trim of more than about a second,
  a clip taken from a longer video, or a different release of a film can break it. Two near-identical
  *static* videos are only grouped if byte-identical. Images under ~32 px are unreliable.
- **Videos with a 10% crop or a changed aspect ratio can still match**; look at the group warning.
- **HEIC / HEIF / AVIF are skipped** with a message (no safe pure-Go decoder; planned via macOS ImageIO).
- Animated GIF/WebP are fingerprinted by their first frame.
- Thresholds were calibrated on synthetic images and a small set of generated videos. Validate on
  your own library before acting on results; the tool never deletes anything for you.
- Embedded image/video preview and playback, MPV, and a persistent SQLite index are deferred to v2. The
  **Preview** button opens the file in its native app instead.

## External drives

- **One reader at a time.** Under `/Volumes/` veyra reads one file at a time. Several readers at different
  places make a hard disk seek constantly, which is slower overall and, on a bus-powered USB drive, can drop the
  connection (macOS logs `Device not configured` and unmounts the volume, then mounts it again).
- **Disconnects are survived, not skipped.** If the volume disappears mid-scan, veyra pauses, shows
  `Drive disconnected: waiting for /Volumes/… to come back (Ctrl+C to stop)` with the spinner running,
  and when the drive is mounted at the same path again it retries the file it was on and carries on. A scan
  that waited reports how many times at the end. Ctrl+C during the wait stops the scan and reports it as partial.
- If the drive comes back under a different name (for example `HD1 1`), veyra cannot find it and keeps
  waiting: stop it, make sure the old mount point is gone, and run it again.

## Safety

Files are opened read-only (the single exception is the confirmed move-to-Trash above); symlinks and hard links are never followed twice; no shell is ever used
(every command is an argument array and paths must be absolute and exist); child processes (`ffprobe`,
`ffmpeg`, Quick Look) have timeouts and die with the scan or the TUI. An interrupted scan reports
itself as partial and exits 130.

## Layout

`cmd/veyra` entry point · `internal/cli` flags · `menu` interactive launcher · `platform` open/reveal/trash/copy per OS · `scanner` walk + classify · `classify` content sniffing ·
`exact` BLAKE3 cascade · `imgfp` / `vidfp` fingerprints · `similarity` candidate search + verification ·
`cluster` complete linkage · `result` UI-independent groups · `app` pipeline · `tui` Bubble Tea UI ·
`progress` status line · `prof` optional timings
(`VEYRA_PROFILE=1` prints a timing table, `VEYRA_CPUPROFILE=file` writes a Go CPU profile).

## Testing

```
go vet ./... && go test -race ./...
go test -run '^$' -bench . ./internal/exact ./internal/imgfp ./internal/similarity
```

Video tests generate fixtures with `ffmpeg` and skip when it is missing.

## Dependencies

Bubble Tea, Bubbles, Lip Gloss (MIT) · `golang.org/x/image`, `golang.org/x/sys` (BSD-3-Clause).
FFmpeg is a runtime requirement and is not bundled; its licence depends on your build.
Licences were taken from each project's repository; re-check before redistributing.
