# VEYRA — Cross-Platform Engineering Agent Specification (v1)

You are the senior Go systems engineer, CLI architect, and terminal-UI engineer responsible for building **`veyra`** (Visual Exploration & Yield-based Recognition Assistant; formerly `dedup`).

Your mission is to build a reliable, local-first, cross-platform application that scans a user-selected directory for duplicate and visually similar **videos and images**, groups the matches, and lets the user inspect them in an interactive terminal UI.

**Status of the project (read this first).**

- **Name and module:** the product and executable are `veyra`; Go module `github.com/RishiKendai/veyra`; repository `https://github.com/RishiKendai/veyra`. Any `dedup` in older text below means `veyra`.
- **Platforms:** macOS, Linux and Windows (amd64 and arm64) are supported release targets. macOS is the reference platform and the only one that has been run by hand; Linux and Windows are covered by unit tests, cross-compilation and CI, and must be described as such until confirmed on real machines. Native actions live in `internal/platform` and are chosen per OS at run time.
- **Video matching:** exact bytes (BLAKE3) or whole-video visual similarity (16 sampled frames, see 6.3). The earlier "different cut / keyframe" tier was removed on purpose: clips taken from longer videos and different releases of a film are not detected, and the code, caches and docs no longer mention it.
- **Entry points:** flags, `veyra help`, `veyra version`, and an interactive menu when run with no arguments (section 2).
- **Distribution:** see section 18.

Read this complete specification before changing code. Inspect the repository and environment first. Do not assume previous code is correct. Reuse proven algorithms and libraries where appropriate, and inspect licenses before adopting dependencies or copying implementation ideas.

---

# 1. Product definition

`veyra` is a focused media duplicate discovery and review tool.

It has one primary operation:

1. Accept a directory and media type.
2. Scan the requested directory.
3. Discover media files.
4. Detect exact duplicates and visually similar/re-encoded copies.
5. Group matching files.
6. Open an interactive TUI to review groups and individual files.
7. Preview selected media and inspect metadata.
8. Allow the user to open a file in its native macOS application or reveal it in Finder.

It is not a generic file manager, cloud service, web application, AI agent, or automatic cleanup tool.

**v1 supports only videos and images.** Do not implement generic file (`file`), audio, document, or arbitrary binary deduplication in v1.

Scanning and review must be read-only. Never delete, move, rename, rewrite, or modify source media.

**Single exception (approved by the user):** in the TUI, the user may press `D` (or click the red Delete button) on a file row, confirm with Enter, and the file is moved to the macOS Trash through Finder (recoverable, never a permanent delete). Nothing else may modify, move or delete files, and nothing is ever deleted without that explicit per-file confirmation.

---

# 2. Required CLI contract

The executable name is `veyra`. There are no subcommands in v1, with three exceptions that the user approved: `veyra` with no arguments (interactive menu), `veyra help` (same as `-h`) and `veyra version` (also `--version`). `help` and `version` are accepted only as the single argument.

Required forms:

```bash
veyra -t video /Users/John/Movies
veyra -t video -r /Users/John/Movies

veyra -t image /Users/John/Pictures
veyra -t image -r /Users/John/Pictures
veyra -t image .                  # the current folder

veyra help                        # same as veyra -h
veyra version

veyra                             # interactive menu (see below)
```

### Interactive menu (`veyra` with no arguments)

- Only when stdin and stdout are terminals. Otherwise print the usage to stderr and exit 2, so scripts never hang.
- Screen 1 offers: scan images, scan images (recursive), scan videos, scan videos (recursive), quit. Keys: arrows / j k, Enter, digits 1-4, Q / Esc / Ctrl+C. A video choice first checks that FFmpeg and FFprobe are installed and shows the install command if not.
- Screen 2 is a folder browser starting in the current folder: Enter opens a folder, Left / Backspace goes up, `S` scans the current folder, `Tab` switches to typing or pasting a path (`~` and relative paths work), `.` toggles hidden folders, Esc goes back.
- The chosen path goes through the same `cli.ValidateDir` as the command line, then the same scan and review flow runs. Code: `internal/menu`.

## Flags

| Flag | Required | Meaning |
|---|---:|---|
| `-t` | Yes | Media type: `video` or `image` |
| `-r` | No | Recursively scan nested directories |
| `-h` | No | Print help and exit |

Rules:

- The scan root is a positional argument, not a required `--path` flag.
- `-t` accepts exactly `video` or `image` in v1. Do not add `file`, `all`, audio, or document types.
- `-r` is optional.
- Without `-r`, inspect files directly inside the given directory only. Do not descend into child directories.
- With `-r`, inspect the directory and all permitted nested directories.
- Paths with spaces and Unicode must work when correctly quoted by the shell.
- Validate that the supplied path exists, is a directory, and is readable.
- Reject missing paths, extra positional arguments, invalid type values, and invalid flag combinations with clear help-oriented errors.
- `-h` must work without requiring `-t` or a path.
- Use standard Unix exit conventions where practical: zero for successful scans (including no matches), nonzero for invalid arguments or fatal scan failures.
- Send progress, diagnostics, and warnings to stderr. Keep the TUI on the terminal and avoid mixing logs into its rendering.
- Do not add flags such as `--json`, `--html`, `--exclude`, `--force`, or cache-management commands in v1 unless explicitly approved. Keep internal design extensible, but do not expand the CLI scope.

Example help:

```text
veyra — find duplicate and visually similar media

Usage:
  veyra -t <video|image> [-r] <directory>

Options:
  -t    Media type to scan: video or image (required)
  -r    Recursively scan nested directories
  -h    Show help

Examples:
  veyra -t video ~/Movies
  veyra -t video -r /Volumes/Media
  veyra -t image -r ~/Pictures
```

---

# 3. Primary user experience: interactive TUI

After a successful scan, open a full-screen interactive terminal UI using **Bubble Tea**.

The TUI is the primary interface. Do not default to printing a plain text report and exiting.

Use a two-column layout:

- **Left panel:** search, duplicate groups, and files within each group.
- **Right panel:** two vertically stacked panels:
  - Top: preview of the currently hovered/selected file.
  - Bottom: metadata for that file.

The UI must be responsive to terminal resizing. Use sensible minimum dimensions and a graceful small-terminal fallback.

## 3.1 Layout sketch

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ VEYRA                                         VIDEO SCAN COMPLETE            │
│ /Users/John/Movies · 2,450 files · 12 groups · 37 matched files              │
├────────────────────────────────┬─────────────────────────────────────────────┤
│ GROUPS / FILES                 │ PREVIEW                                     │
│                                │                                             │
│ Search: holiday_               │                                             │
│                                │          [video/image preview]              │
│ ▼ GROUP 01 · EXACT             │                                             │
│   > video.mp4                  │          video playback controls            │
│     video-copy.mkv             │                                             │
│                                ├─────────────────────────────────────────────┤
│ ▼ GROUP 02 · VISUAL             │ METADATA                                    │
│     holiday.mov                │ Name:       holiday.mov                     │
│     holiday-copy.mp4           │ Location:   /Users/John/Movies/holiday.mov  │
│     holiday.webm               │ Size:       1.2 GB                          │
│                                │ Modified:   2026-10-20                      │
│ ▶ GROUP 03 · EXACT             │ Resolution: 1920 × 1080                     │
│                                │ Duration:   02:15                           │
│                                │ Codec:      H.264                           │
├────────────────────────────────┴─────────────────────────────────────────────┤
│ ↑↓ Navigate  Enter Open  P Play/Pause  O Finder  Space Expand  / Search  Q   │
└──────────────────────────────────────────────────────────────────────────────┘
```

The sketch is conceptual. Implement a clean, compact, usable layout rather than copying exact dimensions.

## 3.2 Left panel: groups and files

The left panel must show:

- All detected duplicate groups.
- Group number or stable group label.
- Match classification, at minimum `EXACT` or `VISUAL`.
- Number of files in each group.
- Expand/collapse state.
- File entries within expanded groups.
- Search input for fuzzy filtering groups and files.

Only show groups with at least two members. Do not list every scanned file as a result.

Group/file navigation:

- Up/Down: move selection.
- Left: collapse current group or move to parent group.
- Right: expand current group or move into its files.
- Space: toggle group expansion where appropriate.
- `/`: activate fuzzy search.
- Esc: clear search or leave search mode.
- Mouse click: select a group or file.
- Mouse hover: update the preview candidate without necessarily changing the committed selection.

Keep **hovered item** and **selected item** as separate state. A transient mouse movement must not accidentally change the file that Enter will open. Keyboard navigation should update the selected item and preview.

Fuzzy search should filter the visible tree without destroying the underlying group data. Use a maintained fuzzy-search component/library where appropriate, or implement a small tested matcher.

## 3.3 Right top: media preview

The top-right panel displays the selected or hovered media.

### Image preview

- Render the actual image in the preview area when the terminal supports a suitable graphics protocol.
- Preserve aspect ratio.
- Fit within the available preview region.
- Do not upscale tiny images unnecessarily.
- For unsupported image formats or terminals, show a clear fallback with filename, format, and a message explaining how to open it natively.
- Avoid rendering a giant image that breaks the layout.

### Video preview and playback

The user explicitly wants video to play inside the terminal preview panel, not merely show a thumbnail.

Treat embedded video playback as a core v1 objective, subject to terminal capability.

Preferred implementation:

1. Use FFmpeg/FFprobe for media decoding and metadata.
2. Decode preview frames in a controlled background worker.
3. Render frames in the right preview area using a supported terminal graphics protocol.
4. Keep playback state separate from Bubble Tea's main model.
5. Coordinate rendering with Bubble Tea so video frames do not corrupt the rest of the TUI.
6. Support basic controls: play/pause, seek backward/forward where feasible, restart, and stop when selection changes.
7. Limit preview resolution and frame rate to avoid excessive CPU, memory, and terminal bandwidth.
8. Stop decoding promptly when the user changes selection, exits the TUI, or cancels playback.
9. Do not launch an unbounded FFmpeg process for every mouse-hover event. Debounce preview changes and cancel obsolete work.

### Terminal graphics compatibility

Do not assume all terminals support inline images or video.

At startup, detect terminal capabilities conservatively. Consider macOS terminal environments such as:

- iTerm2 and its inline image protocol.
- Kitty and its graphics protocol.
- WezTerm where supported.
- Other terminals only when their protocol is verified.

Do not claim support for a terminal protocol without testing it.

Use a renderer abstraction, for example:

```go
type PreviewRenderer interface {
    DetectCapabilities() Capabilities
    RenderImage(...)
    RenderVideoFrame(...)
    ClearPreview(...)
}
```

The exact interface can change after architecture review.

If inline graphical preview is unavailable:

- Show a representative video thumbnail or image description/path in the panel.
- Provide a clear native-open action.
- Do not crash or corrupt the TUI.
- Do not claim embedded playback is available when it is not.

### MPV's role

MPV may be used as an optional fallback for playback outside the embedded preview region. It is not a substitute for the requested embedded preview.

Important: MPV terminal video output does not automatically stay confined to Bubble Tea's right-hand panel. Do not simply start MPV's terminal output on top of the TUI and call it embedded playback.

A separate native MPV window is acceptable as a fallback, but the normal supported-terminal experience should aim for playback inside the right preview panel.

## 3.4 Right bottom: metadata panel

Show metadata for the currently selected file, adapting fields to the media type.

Common fields:

- Filename.
- Full absolute path.
- File size (human-readable).
- File extension (as observed; do not treat it as authoritative type).
- Detected media format/container.
- Created timestamp, if reliably available.
- Modified timestamp.
- File dimensions for images.
- Image color space/bit depth where available.
- Video duration.
- Video resolution.
- Video codec.
- Frame rate, when available.
- Video bitrate, when available.
- Audio stream presence and codec, when available.
- Duplicate group ID.
- Match classification.
- Similarity score or confidence, when meaningful.

Do not invent metadata. If a value is unavailable or unreliable on macOS, display `Unknown` or omit it. Clearly distinguish filesystem timestamps from embedded media timestamps.

Avoid allowing long paths or metadata values to break the panel; truncate visually and allow a way to inspect/copy the full path.

---

# 4. User actions and key bindings

Initial keymap:

| Key/action | Behavior |
|---|---|
| Up / Down | Navigate visible groups/files |
| Left / Right | Collapse/expand group or navigate tree |
| Space | Expand/collapse group |
| `/` | Fuzzy search |
| Esc | Clear search / leave search |
| Enter | Open selected file in its default macOS application |
| `P` | Play/pause selected video preview |
| `O` | Reveal selected file in Finder |
| `R` | Restart selected preview, if useful |
| `Q` | Quit |
| Mouse click | Select item |
| Mouse hover | Update preview candidate |

Document the final keymap in the UI footer and help screen.

### Native actions (per OS)

Use argument-safe process execution; never concatenate user-controlled paths into shell command strings.

| Action | macOS | Linux | Windows |
|---|---|---|---|
| Open (Enter) | `open <path>` | `xdg-open <path>` | `rundll32 url.dll,FileProtocolHandler <path>` |
| Reveal (O) | `open -R <path>` | D-Bus `FileManager1.ShowItems`, else `xdg-open <folder>` | `explorer /select,<path>` |
| Move to Trash (D) | Finder via `osascript` (path as argv) | `gio trash`, `trash-put` or `trash` | Recycle Bin via PowerShell, path as base64 on stdin |
| Copy path (C) | `pbcopy` | `wl-copy`, `xclip` or `xsel` | PowerShell `Set-Clipboard`, text as base64 on stdin |
| Quick Look (P) | `qlmanage -p <path>` | not available | not available |

The TUI asks the platform for the file manager's name and for Quick Look availability, and hides what does not exist. A path is only ever passed as a separate argument or as encoded data on stdin, never inside script text.

Use Go's `exec.Command` with separate arguments. Never invoke a shell just to open a file.

If launching a native application fails, show a useful error in the TUI.

---

# 5. Media discovery: inspect content, not just extensions

A core requirement is to find media regardless of filename or extension.

Extensions can be used as a fast hint, but cannot be the only source of truth.

## Video discovery

- Probe candidate files using FFprobe or a suitable decoder.
- A video with a misleading or missing extension should still be discoverable if it contains a supported decodable video stream.
- A file named `.mp4` that is not actually a video must not be classified as video solely because of its extension.
- Detect actual video streams in containers.
- Handle multiple streams.
- Mark unsupported, corrupt, encrypted, or partially readable files as skipped with a reason.
- Never crash the whole scan because one media file is malformed.

## Image discovery

- Decode candidate files using supported image decoders.
- An image with a misleading or missing extension should be recognized when its content can be decoded.
- A file with `.jpg` in its name that is not a valid image must not be accepted solely based on its extension.
- Normalize EXIF orientation for fingerprinting and preview.
- Handle animated formats deliberately. For v1, define whether animated GIF/WebP is fingerprinted using a representative frame or multiple frames, and document the choice.
- Support common formats through maintained decoders: JPEG, PNG, GIF, WebP, TIFF, BMP where practical.
- HEIC/HEIF is important on macOS/iPhone libraries. Investigate the safest implementation:
  - Prefer a well-maintained decoder or a small macOS ImageIO adapter if needed.
  - Do not silently claim HEIC support if it is unavailable.
  - Document any extra runtime dependency or platform-specific implementation.
- Preserve original files; normalization happens only in memory.

For this macOS-first v1, it is acceptable to use a small, isolated macOS-specific helper for formats or preview capabilities that lack a suitable pure-Go implementation. Keep it out of the core matching interfaces.

---

# 6. Duplicate detection

The application must distinguish exact duplicates from perceptually similar media.

## 6.1 Exact duplicates

Use streaming BLAKE3 over complete file contents.

- Identical bytes must match regardless of filename or extension.
- File size can be used as a safe prefilter for exact-byte hashing because identical byte streams necessarily have equal lengths.
- Never declare exact duplicates based on size alone.
- Do not load entire files into RAM.
- Hash using bounded buffers.
- Verify content before classifying an exact match.

Exact duplicate classification is `EXACT`.

## 6.2 Image similarity

Use decoded pixel content, not filenames, extensions, or compressed file bytes.

Recommended staged approach:

1. Exact BLAKE3 matching.
2. Decode and normalize image orientation.
3. Generate perceptual fingerprints using proven algorithms:
   - aHash for a fast coarse signal.
   - dHash for gradient information.
   - pHash (DCT-based) for perceptual similarity.
4. Compare fingerprints using Hamming distance.
5. Calibrate thresholds with a representative test dataset.
6. For difficult crops, rotations, mirrored images, watermarks, or overlays, consider additional matching only after core pHash behavior is measured.

Do not equate semantic similarity with duplication. Two different photos of the same person, pet, building, or scene are not necessarily duplicates.

Classifications:
- `EXACT`: byte-identical.
- `VISUAL`: same image or substantially equivalent image after resizing/re-encoding/limited changes.
- Do not create a broad semantic `RELATED` category in v1.

## 6.3 Video similarity

Use FFprobe for metadata and FFmpeg for frame decoding. Do not re-encode or modify original videos.

Required approach:

1. Exact BLAKE3 matching.
2. Probe actual video stream and duration.
3. Extract representative frames at normalized timestamps or time windows.
4. Resize frames in memory for fingerprinting.
5. Generate robust perceptual frame hashes.
6. Compare sequences, not only a single thumbnail.
7. Use temporal alignment or sliding windows to account for small trims and offset starts where feasible.
8. Consider duration, frame rate, resolution, and codec as evidence, not proof.
9. Account for re-encoding, compression artifacts, different resolutions, frame-rate changes, letterboxing, and aspect-ratio changes.
10. Avoid declaring matches solely because a few static/repeated frames look alike.

A first implementation can use a bounded number of representative frames, but the design must not hard-code the assumption that five frames can detect every duplicate. Make sampling strategy explicit, testable, and extensible.

The matcher must be conservative. False positives are dangerous because users may later decide which files to retain.

Classification:
- `EXACT`: byte-identical.
- `VISUAL`: sufficiently strong evidence that the clips are the same or substantially overlapping footage.
- If evidence is ambiguous, do not force the files into a duplicate group.

---

# 7. Grouping and match confidence

The output is groups, not a flat list of matching pairs.

Represent matches as a graph:

- Each media file is a node.
- A verified match is an edge.
- A group is a set of related nodes.

Do not blindly use connected components if that causes transitive false positives.

Example:
- A strongly matches B.
- B weakly matches C.
- A does not match C.

Do not automatically imply A and C are duplicates merely because all three are connected.

Use a deliberate clustering policy, such as representative-based verification or complete-linkage constraints for visual groups.

Each group should retain:
- Stable group ID for the current scan.
- Media type (`video` or `image`).
- Classification (`EXACT` or `VISUAL`).
- Member paths.
- Pairwise or representative scores.
- Any uncertainty/warnings.

Group and file ordering should be deterministic. Keep only groups with two or more members.

Do not display a similarity percentage unless the score has a defined, tested meaning. A raw Hamming distance is not automatically a percentage confidence.

---

# 8. Scanning behavior and filesystem safety

The scanner may inspect external hard disks and large folders.

- Default scan is non-recursive.
- `-r` enables recursive traversal.
- Do not follow directory symlinks by default.
- Avoid symlink loops and traversal outside the chosen root.
- Do not request administrator/root privileges.
- Handle permission-denied directories and disappearing files.
- Handle external disk disconnects gracefully.
- Distinguish a complete scan from a partial scan.
- Do not write temporary files beside source media.
- Use an application-owned cache/temp directory when necessary.
- Never modify source files.
- Never execute discovered files.
- Treat paths and filenames as untrusted data.
- Use context cancellation and clean shutdown.
- Limit FFmpeg/FFprobe subprocesses and processing workers.
- Set subprocess timeouts.
- Kill child processes when the scan is cancelled or the TUI exits.
- Do not spawn an unbounded number of goroutines or media processes.
- Use argument arrays for subprocesses, never shell interpolation.

A cancelled or interrupted scan must not be represented as complete.

---

# 9. Platform integration and dependencies

v1 targets macOS, Linux and Windows. macOS is the reference platform. Platform differences stay in `internal/platform` (native actions, file creation time) and the small `_darwin` / `_other` files for cache bypass, file identity and volume detection. Everything else is portable Go and builds with `CGO_ENABLED=0`.

Known gaps outside macOS: external drives are only recognised under `/Volumes/`, so the one-reader-at-a-time rule for hard disks is not applied on Linux or Windows; the file creation time is shown as Unknown on Linux.

## Required development environment

- Go toolchain.
- macOS, Linux or Windows.
- Git.
- `make` (optional; the Makefile wraps the commands in section 18).
- FFmpeg and FFprobe available in PATH for video scanning and frame extraction.

Typical installation:

```bash
brew install go ffmpeg
```

## Optional runtime dependency

MPV may be installed for a separate native playback fallback:

```bash
brew install mpv
```

MPV is optional. Do not make the core scanner depend on it.

## Go libraries and tools

Evaluate and use maintained libraries where appropriate:

- Bubble Tea: TUI application framework.
- Bubbles: reusable TUI components, including list/text input/viewport where useful.
- Lip Gloss: TUI styling.
- A maintained fuzzy-search implementation or Bubbles filtering.
- `github.com/zeebo/blake3`: exact content hashing.
- Go image packages plus maintained image decoders.
- A maintained pHash implementation or a carefully tested implementation.
- SQLite with a pure-Go driver if persistent indexing is adopted.

Prefer pure-Go dependencies where practical to simplify builds. Do not add dependencies merely for convenience if the standard library is sufficient.

Inspect dependency maintenance, API stability, license, and macOS compatibility before adoption.

## Native actions

See the per-OS table in section 4.

Do not use shell command construction. Use `exec.Command` with explicit arguments.

---

# 10. TUI implementation architecture

Use Bubble Tea as the main event loop.

Keep the TUI responsive. Do not run scanning, hashing, FFmpeg decoding, image decoding, or filesystem traversal synchronously inside `Update`.

Use Bubble Tea commands/messages or controlled worker orchestration to send progress/results to the model.

Suggested conceptual modules:

```text
cmd/
  veyra/
internal/
  app/
  cli/
  menu/
  scanner/
  classifier/
  metadata/
  exact/
  image/
  video/
  fingerprint/
  similarity/
  clustering/
  index/
  tui/
    model/
    layout/
    components/
    preview/
      renderer/
      image/
      video/
    styles/
  platform/
    (open, reveal, trash, copy for macOS, Linux and Windows)
  process/
  safety/
  model/
```

Suggested data flow:

```text
CLI arguments
    |
    v
Validate media type and scan root
    |
    v
Filesystem scanner
    |
    v
Content classification
    |
    v
Metadata extraction
    |
    +------------------------------+
    |                              |
    v                              v
Exact BLAKE3                 Category-specific engine
    |                         /                 \
    |                      Image                Video
    |                       |                    |
    |                   Decode pixels       FFprobe/FFmpeg
    |                       |                    |
    |                   pHash/dHash        Frame fingerprints
    |                       |                    |
    +-----------------------+--------------------+
                            |
                            v
                    Similarity matcher
                            |
                            v
                        Clustering
                            |
                            v
                    Result data model
                            |
                            v
                     Bubble Tea TUI
                       /          \
                      v            v
                 Left groups    Right preview
                 and files      and metadata
```

## Separation of concerns

- Scanner discovers paths and file identities.
- Classifier determines actual media category.
- Metadata layer extracts type-specific information.
- Exact engine calculates byte hashes.
- Image engine decodes and fingerprints images.
- Video engine probes, decodes, samples, and fingerprints frames.
- Similarity layer compares fingerprints.
- Clustering layer creates groups.
- Result model is independent of UI.
- TUI renders groups and handles navigation.
- Preview renderer handles image/video rendering and terminal capabilities.
- Platform adapter (`internal/platform`) handles native opening, reveal in the file manager, Trash and clipboard on each OS.

Do not build a monolithic `main.go` or mix FFmpeg logic into Bubble Tea event handlers.

---

# 11. Preview rendering engineering requirements

This is a high-risk technical area and must be prototyped early.

Before committing to an embedded video implementation:

1. Identify the exact terminal(s) available in the development environment.
2. Test the terminal's graphics protocol and image rendering behavior.
3. Test Bubble Tea mouse support and alternate-screen behavior.
4. Prototype rendering an image inside a reserved preview area.
5. Prototype rendering a sequence of decoded video frames in that same area.
6. Verify that frame updates do not erase the left panel, metadata panel, footer, or borders.
7. Verify resizing, scrolling, selection changes, pause, stop, and quit behavior.
8. Measure CPU usage, frame rate, and terminal output volume.

Potential protocols include:
- iTerm2 inline images.
- Kitty graphics protocol.
- Other protocols only if actually supported by the target terminal.

Do not assume terminal capability from `$TERM` alone. Use conservative detection and, where safe, a small explicit capability probe or user-configurable override.

Important architectural warning:
- Bubble Tea normally owns terminal rendering.
- A separate process writing video frames directly to stdout/stderr can corrupt the TUI.
- Do not allow FFmpeg or MPV to write uncontrolled terminal output.
- Design a preview renderer that coordinates with the TUI's rendering lifecycle.
- If a protocol cannot be safely integrated with Bubble Tea's renderer, document the limitation and use a controlled fallback rather than shipping a broken preview.

Fallback hierarchy:
1. Embedded graphical preview/playback in a supported terminal.
2. Static poster frame inside the TUI plus a clear indication that inline playback is unsupported.
3. User presses Enter to open the original video in its native macOS application.
4. Optional MPV native window fallback if installed and explicitly selected.

The fallback must never make the TUI unusable.

---

# 12. Performance and resource management

The target may be a 2 TB external hard disk.

- Use bounded worker pools.
- Do not create one unbounded goroutine per file.
- Stream BLAKE3 using fixed-size buffers.
- Do not load entire videos into memory.
- Extract only a bounded number of frames for initial comparison.
- Resize decoded frames in memory for hashing.
- Avoid decoding full-resolution frames if unnecessary.
- Make worker counts conservative and tune using benchmarks.
- Account for HDD versus SSD behavior; more concurrency can make a spinning disk slower.
- Limit simultaneous FFmpeg/FFprobe processes.
- Debounce preview updates when the user rapidly moves through files.
- Cancel obsolete preview work immediately.
- Reuse metadata/fingerprints within the current scan.
- Consider SQLite persistent indexing only after the core scan and matching behavior is correct.
- If an index is implemented, validate cached entries and reprocess changed files. Metadata alone must not be treated as cryptographic proof that content is unchanged.
- Ensure interrupted scans cannot leave a false complete state.

---

# 13. Testing requirements

Create automated tests and media fixtures.

## CLI tests

- Missing `-t`.
- Invalid type.
- Missing path.
- Nonexistent path.
- Path points to a file rather than directory.
- Non-recursive behavior.
- Recursive behavior.
- Spaces and Unicode in path.
- `-h` without other arguments.
- Extra arguments.
- No duplicate groups.

## Exact duplicate tests

- Same bytes, different filenames.
- Same bytes, different extensions.
- Same bytes in nested folders.
- Same-size files with different bytes.
- Same filename with different bytes.

## Image tests

- Same image in JPEG and PNG.
- Same image at different resolutions.
- Same image with different compression quality.
- EXIF orientation.
- Slight brightness/color changes.
- Crop and mirror cases documented as supported or unsupported.
- Two different photos of the same subject must not automatically become duplicates.
- Corrupt image.
- Unsupported image format.
- HEIC/HEIF behavior on macOS.

## Video tests

- Same video, different filename.
- Same video, different container where decoding permits.
- Same video re-encoded at another resolution/bitrate.
- Different codec.
- Different compression level.
- Misleading or missing extension.
- Two unrelated videos with same duration and resolution.
- Slightly trimmed copy.
- Different videos containing the same static scene.
- Corrupt/truncated video.
- Variable frame rate.
- Video with no audio stream.
- FFmpeg/FFprobe unavailable.

## TUI tests

- Group list navigation.
- Expand/collapse.
- Fuzzy search.
- Hover updates preview candidate.
- Selection remains stable when pointer moves.
- Metadata updates for selected file.
- Terminal resize.
- Small terminal fallback.
- Preview cancellation on selection change.
- Preview stops on quit.
- Unsupported graphics protocol fallback.
- Native open failure handling.
- Finder reveal failure handling.

Test both true positives and false positives. Do not tune similarity thresholds only to pass a handful of hand-selected examples.

---

# 14. Open-source research and licensing

Before implementing perceptual hashing or video matching from scratch, inspect:

- Czkawka/Krokiet: https://github.com/qarmin/czkawka
- Video Duplicate Finder: https://github.com/hclivess/video-duplicate-finder
- dupeGuru: https://github.com/arsenetar/dupeguru
- FFmpeg documentation: https://ffmpeg.org/documentation.html
- Bubble Tea: https://github.com/charmbracelet/bubbletea
- Bubbles: https://github.com/charmbracelet/bubbles
- Lip Gloss: https://github.com/charmbracelet/lipgloss
- MPV manual: https://mpv.io/manual/stable/

For each candidate library or implementation, record:
- License and compatibility.
- Maintenance/activity.
- macOS support.
- Accuracy characteristics.
- Performance characteristics.
- Whether it can be used as a library or only as a reference.
- Any native/runtime dependencies.

Do not copy source code without checking license obligations.

FFmpeg licensing depends on the build configuration and enabled codecs. If distributing FFmpeg with `veyra`, review the exact build's licensing requirements. For v1, it is acceptable to require users to install FFmpeg separately and detect it at runtime.

---

# 15. Implementation phases

Do not attempt to build everything at once. Work in reviewable phases.

## Phase 0 — Repository and environment inspection

- Inspect repository structure and current implementation.
- Inspect Go version and macOS version.
- Determine CPU architecture (Apple Silicon or Intel).
- Check whether FFmpeg, FFprobe, MPV, and the target terminal are available.
- Identify current terminal graphics capabilities.
- Inspect existing code and tests before modifying anything.
- Research existing projects and dependency licenses.
- Propose architecture, dependencies, risks, and milestones.
- Wait for approval before major changes.

## Phase 1 — CLI and scanner

Implement:

```bash
veyra -t video /path
veyra -t video -r /path
veyra -t image /path
veyra -t image -r /path
veyra -h
```

Validate recursive/non-recursive behavior, content-based classification, path handling, and safe errors.

## Phase 2 — Exact duplicate engine

Implement streaming BLAKE3 and deterministic grouping for videos and images. This phase must work without perceptual similarity.

## Phase 3 — Image similarity

Implement image decoding, orientation normalization, perceptual hashes, calibrated thresholds, and tests.

## Phase 4 — Video similarity

Integrate FFprobe/FFmpeg, bounded frame extraction, perceptual frame hashes, and temporal matching.

## Phase 5 — Result model and clustering

Implement conservative grouping and stable result models independent of the UI.

## Phase 6 — TUI foundation

Build the Bubble Tea two-panel interface:
- Left: groups/files/search.
- Right top: preview placeholder and image rendering.
- Right bottom: metadata.
- Keyboard and mouse interactions.
- Responsive layout.

## Phase 7 — Video preview prototype and integration

Prototype terminal graphics playback early enough to expose protocol limitations. Implement embedded preview only when it can be safely coordinated with Bubble Tea. Provide native playback fallback.

## Phase 8 — macOS integration and hardening

Implement open, Finder reveal, Quick Look fallback, dependency checks, cancellation, resource cleanup, performance tests, and documentation.

## Phase 9 — Optional persistent index

Only after correctness and UX are established, add SQLite caching/incremental scanning if it materially improves repeated scans. Do not let indexing delay a working v1.

At the end of each phase:
- Run `gofmt`.
- Run unit and integration tests.
- Run static checks.
- Report implemented behavior.
- Report known limitations.
- Show CLI usage and sample TUI behavior.
- Do not claim completion if tests were skipped or failing.

---

# 16. Definition of done for v1

The first release is complete when:

1. `veyra -t video /path` scans immediate files only.
2. `veyra -t video -r /path` recursively scans videos.
3. `veyra -t image /path` scans immediate image files only.
4. `veyra -t image -r /path` recursively scans images.
5. `veyra -h` shows correct help.
6. Discovery is based on actual decodable media content, not extensions alone.
7. Exact duplicates are detected by complete content hash.
8. Re-encoded/resized video copies can be detected by frame-content matching within documented limits.
9. Resized/re-encoded image copies can be detected by perceptual matching within documented limits.
10. False positives are controlled and tested.
11. Results are grouped deterministically.
12. The TUI has the specified left groups/files panel and right preview/metadata panels.
13. Keyboard and mouse navigation work.
14. Fuzzy search works.
15. Hover/selection updates the preview without accidental open actions.
16. Images preview in supported terminals, with graceful fallback.
17. Video playback works inside explicitly supported terminals, or a clearly documented capability fallback is provided. Do not claim universal inline playback.
18. Enter opens the selected original file in its default macOS application.
19. O reveals the selected file in Finder.
20. The application never modifies source media.
21. Cancellation and errors clean up subprocesses and temporary resources.
22. Tests cover the important correctness, safety, and UI cases.

---

# 17. Required first response from the agent

Before coding, respond with:

1. Your understanding of the exact CLI contract.
2. Your understanding of the two-panel TUI and interactions.
3. Proposed Go package architecture.
4. Proposed Go dependencies and why.
5. FFmpeg/FFprobe integration strategy.
6. Image and video duplicate detection strategy.
7. A realistic terminal video playback prototype plan for the detected macOS terminal.
8. macOS image format support plan, including HEIC/HEIF.
9. Performance strategy for a 2 TB external disk.
10. Filesystem safety strategy.
11. Open-source projects and licenses inspected.
12. Phased implementation plan with testable acceptance criteria.
13. Risks, unknowns, and trade-offs.

**Do not begin broad implementation until the user approves the architecture and the terminal preview feasibility plan.**

Act as an engineering partner, not a code generator. Challenge flawed assumptions, explain trade-offs, and prioritize correctness, safety, maintainability, and measured performance over premature optimization.


---

# 18. Build, release and distribution

The user builds and publishes from the repository root. A tag `vX.Y.Z` pushed to GitHub produces the release.

## Local commands (`Makefile`)

| Command | Effect |
|---|---|
| `make dev` / `make dev ARGS="-t image ."` | `go run ./cmd/veyra` (no args = interactive menu) |
| `make test` | `go test ./... -count=1` |
| `make test-dev` | gofmt check, `go vet`, tests with `-race` |
| `make build` | `./veyra` for this machine |
| `make dist` | all six archives plus `dist/checksums.txt` (`dist-mac`, `dist-linux`, `dist-windows` build one OS) |
| `make clean` | remove `dist/` and built binaries |

`VERSION` defaults to the git tag (or `dev`); override with `make dist VERSION=v0.1.0`.

## Release artifacts

- Names are fixed: `veyra_<darwin|linux>_<amd64|arm64>.tar.gz`, `veyra_windows_<amd64|arm64>.zip`, `checksums.txt`. No version in the file name, so `releases/latest/download/<name>` URLs are stable. `install.sh`, `install.ps1`, `.goreleaser.yaml` and the Makefile must agree on these names.
- Unix archives contain `veyra`, `install.sh`, `README.md`. Windows zips contain `veyra.exe`, `install.ps1`, `install.cmd`, `README.md`.

## Files

| File | Purpose |
|---|---|
| `.goreleaser.yaml` | Release recipe: builds, archives, checksums, Homebrew formula |
| `.github/workflows/ci.yml` | Tests and cross-compile on every push / pull request |
| `.github/workflows/release.yml` | Runs GoReleaser when a `v*` tag is pushed |
| `install.sh` | macOS / Linux installer: installs the bundled binary, or downloads and checksum-verifies the latest release; puts `veyra` on the PATH |
| `install.ps1` | Windows equivalent; adds the install folder to the user PATH |
| `install.cmd` | Double-click launcher for `install.ps1` that bypasses the default script-execution block for that one run |
| `Makefile` | Local dev, test, build and packaging commands |

## Rules

- Installers must verify the SHA-256 checksum of anything they download and refuse on mismatch.
- Installers must never run code from an archive automatically; the user runs the installer.
- Do not claim Linux or Windows behaviour is verified until it has been run on those systems.
- Do not run `git`, push, tag, or publish without the user's explicit request.
