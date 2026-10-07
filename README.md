# comfymeta

[![CI](https://github.com/gil-x/comfy-metadata-extractor/actions/workflows/ci.yml/badge.svg)](https://github.com/gil-x/comfy-metadata-extractor/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/gil-x/comfy-metadata-extractor)](https://github.com/gil-x/comfy-metadata-extractor/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/gil-x/comfy-metadata-extractor.svg)](https://pkg.go.dev/github.com/gil-x/comfy-metadata-extractor)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Recover the generation parameters of any image, video or audio file made with [ComfyUI](https://github.com/comfyanonymous/ComfyUI).**

ComfyUI stores the full graph that produced each file in the file's metadata. `comfymeta` reads it and shows what matters: model, prompts, seed, sampler, dimensions, duration, BPM… It can also export the raw `prompt` and `workflow` JSON so you can load them back into ComfyUI.

- Supports **PNG**, **MP4/MOV** and **MP3**
- Works on one file, many files or whole directories (recursively if needed)
- Human-readable output, structured JSON, or export to files
- Single binary, **zero dependencies**: Go standard library only
- Usable from the command line or as a Go library

```text
$ comfymeta testdata/specimen.mp3
File: testdata/specimen.mp3
  Type:            audio
  Model:           ace_step_1.5_turbo_aio.safetensors
  Source audio:    Painkiller - Forest fight.mp3
  Tags:            industrial techno, mechanical percussion, metallic clanks, hydraulic pistons,
                   driving four-on-the-floor kick, distorted analog bass, 128 bpm,
                   relentless, hypnotic, instrumental
  Duration (s):    40
  BPM:             128
  Time signature:  4
  Key:             F minor
  Language:        en
  Seed:            712861949169538
  Steps:           8
  CFG:             1
  Sampler:         euler
  Scheduler:       simple
  Denoise:         1
```

## Installation

### Download a binary

Prebuilt binaries for Windows, macOS and Linux (x86-64 and ARM64) are on the [Releases page](https://github.com/gil-x/comfy-metadata-extractor/releases/latest).

| System                        | Archive                                  |
|-------------------------------|------------------------------------------|
| Windows                       | `comfymeta_<version>_windows_amd64.zip`  |
| macOS (Apple Silicon)         | `comfymeta_<version>_macos_arm64.tar.gz` |
| macOS (Intel)                 | `comfymeta_<version>_macos_amd64.tar.gz` |
| Linux                         | `comfymeta_<version>_linux_amd64.tar.gz` |
| Linux ARM (Raspberry Pi 4/5…) | `comfymeta_<version>_linux_arm64.tar.gz` |

Extract the archive and put `comfymeta` (or `comfymeta.exe`) somewhere on your `PATH`. Each release also has a `checksums.txt` file with the SHA-256 of every archive.

The binaries are not code-signed, so the first launch may trigger a warning:

- **macOS**: "cannot be opened because the developer cannot be verified". Remove the quarantine flag once:
  ```sh
  xattr -d com.apple.quarantine ./comfymeta
  ```
- **Windows**: SmartScreen may show "Windows protected your PC". Click **More info**, then **Run anyway**.

### With Go

With Go 1.22 or later:

```sh
go install github.com/gil-x/comfy-metadata-extractor/cmd/comfymeta@latest
```

### From source

```sh
git clone https://github.com/gil-x/comfy-metadata-extractor.git
cd comfy-metadata-extractor
go build -o comfymeta ./cmd/comfymeta
```

## Usage

```text
comfymeta [options] <path> [more paths ...]
```

A path can be a file (`.png`, `.mp4`, `.mov`, `.m4v`, `.mp3`) or a directory. For a directory, every supported file in it is processed. Add `-r` to include subdirectories.

### What to print

| Option      | Output                                                  |
|-------------|---------------------------------------------------------|
| *(none)*    | human-readable summary of the generation parameters     |
| `-summary`  | force the human-readable summary (useful with `-export`) |
| `-json`     | extracted parameters as JSON                            |
| `-prompt`   | the `prompt` JSON (API-format graph), indented          |
| `-workflow` | the `workflow` JSON (editor graph), indented            |
| `-raw`      | every raw metadata entry                                |
| `-keys`     | the list of metadata keys present                       |
| `-version`  | the comfymeta version                                   |

### Where to write it

| Option        | Effect                                                                |
|---------------|-----------------------------------------------------------------------|
| *(none)*      | standard output                                                       |
| `-export`     | one file per source, named after it (`clip_00254_.mp4` → `clip_00254_.json`). Without a content option, exports JSON. |
| `-outdir DIR` | put exported files in `DIR` (created if needed)                       |
| `-r`          | walk subdirectories recursively                                       |

Exported file suffixes: `.json` (`-json`), `.prompt.json`, `.workflow.json`, `.summary.txt`, `.raw.txt`, `.keys.txt`.

### Examples

```sh
# Summary of every render in a directory
comfymeta ~/ComfyUI/output

# Parameters of a whole tree as JSON, one file per render
comfymeta -r -export -outdir ./params ~/ComfyUI/output

# Get a video's workflow to load it back into ComfyUI
comfymeta -workflow clip_00254_.mp4 > clip_00254_.workflow.json

# Pipe the JSON into jq
comfymeta -json image.png | jq -r .seed
```

## Extracted parameters

`comfymeta` works out the kind of generation from the graph's nodes, then reads the matching fields. Missing fields are omitted.

| Type      | Detected from                                     | Fields                                                                                   |
|-----------|---------------------------------------------------|------------------------------------------------------------------------------------------|
| **image** | default (KSampler, SamplerCustom…)                | model, positive/negative prompts, seed, steps, CFG, sampler, scheduler, denoise, dimensions, source images |
| **video** | LTXV, SaveVideo, CreateVideo, VideoCombine… nodes | checkpoint, LoRA, text encoder, latent upscaler, prompts, base pass seed, dimensions, length, FPS, source image |
| **audio** | SaveAudio, VAEDecodeAudio, ACE-Step… nodes        | model, tags, lyrics, duration, BPM, time signature, key, language, KSampler settings, source audio |

Sample `-json` output for a video:

```json
{
  "file": "testdata/specimen.mp4",
  "media_type": "video",
  "model": "10Eros_v1-fp8mixed_learned.safetensors",
  "positive": "Character asks \"You're talking to me?\"",
  "negative": "music, text, subtitles",
  "seed": 529889386378618,
  "width": 640,
  "height": 480,
  "length": 48,
  "fps": 24,
  "distilled_lora": "ltx-2.3-22b-distilled-lora-384.safetensors",
  "text_encoder": "gemma-3-12b-it-heretic-v2_nvfp4.safetensors",
  "latent_upscale_model": "ltx-2.3-spatial-upscaler-x2-1.1.safetensors",
  "loaded_images": [
    "specimen.png"
  ]
}
```

## Where ComfyUI stores its metadata

| Format    | Location                                                                                          |
|-----------|---------------------------------------------------------------------------------------------------|
| PNG       | `tEXt`, `zTXt` and `iTXt` text chunks                                                             |
| MP4 / MOV | QuickTime/ISO metadata table: `moov/udta/meta` (`keys` + `ilst` boxes)                            |
| MP3       | ID3v2 tag at the start of the file, `TXXX` frames (versions 2.2 to 2.4, Latin-1, UTF-16 or UTF-8 text) |

In all three cases there are two entries:

- `prompt`: the API-format graph, the one that actually runs. The extracted parameters come from it.
- `workflow`: the editor graph, with node positions and groups. Drop it into ComfyUI to get the workflow back.

## Using it as a library

```go
import comfymeta "github.com/gil-x/comfy-metadata-extractor"

chunks, err := comfymeta.Read("render.png") // format detected from the file signature
if err != nil {
	log.Fatal(err)
}
if wf, ok := comfymeta.Find(chunks, "workflow"); ok {
	fmt.Println(len(wf.Text), "bytes of workflow")
}
p := comfymeta.Extract("render.png", chunks)
fmt.Println(p.MediaType, p.Model, *p.Seed)
```

## Limitations

- Parameter extraction relies on heuristics: a heavily customized graph (custom nodes, dynamically built prompts) can yield incomplete fields. The raw `prompt` and `workflow` JSON are always available through `-prompt` and `-workflow`.
- With `-export`, two sources with the same name but different extensions (`render.png` and `render.mp4`) write to the same output file.
- WebP, WebM, FLAC and Opus files are not supported yet.

## Development

```text
.
├── cmd/comfymeta/   # the command-line tool (options, export, summary)
├── metadata.go      # public API: Read, Find, Supported
├── png.go           # PNG chunk reader
├── mp4.go           # MP4/QuickTime box reader
├── id3.go           # ID3v2 tag reader (MP3)
├── params.go        # Params and Extract: from graph to parameters
├── graph.go         # "prompt" graph traversal
├── testdata/        # one real ComfyUI render per format
├── .github/workflows/  # CI (tests on every push) and Release (on v* tags)
└── .goreleaser.yaml    # cross-platform release build
```

```sh
go test ./...
go vet ./...
```

### Releasing

CI runs vet and the tests on Linux, macOS and Windows on every push. To publish a release, push a version tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The [Release workflow](.github/workflows/release.yml) then runs [GoReleaser](https://goreleaser.com) ([config](.goreleaser.yaml)), which builds every platform, packages the archives with the README and license, and publishes them on the Releases page with a changelog. To try the build locally without publishing anything: `goreleaser release --snapshot --clean`.

## License

[MIT](LICENSE)
