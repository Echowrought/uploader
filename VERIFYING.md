# Verify a release

This is for geeks. Or those who really want to check where their download came from. You don't need to be part of our team or have permission to change the repository. None of these steps publishes anything or runs the uploader.

There are two different checks:

- **Verify the download:** check that its exact bytes were attested by our release workflow for the linked tag.
- **Rebuild it yourself:** compile that tag's source and compare the resulting executable with the downloaded one.

Attestation alone doesn't independently prove compilation from source. Neither check proves the code is safe or removes Windows SmartScreen warnings.

## 1. Verify the download

Install a current [GitHub CLI](https://cli.github.com/). Download the ZIP you want and `build-info.json` from the same release. Open PowerShell in the folder containing them. For the installer instead of a ZIP, use the installer-verification commands below.

If GitHub CLI asks you to sign in, run `gh auth login` with your own GitHub account. No organization membership or write access is needed.

Set the tag to the release you're checking. Use `cwg-rpg-uploader-console` instead of `cwg-rpg-uploader` below if you downloaded the console version:

```powershell
$tag = "v0.1.3" # Replace with the release tag.
$variant = "cwg-rpg-uploader"
$repository = "cwg-rpg-community/uploader"
$workflow = "$repository/.github/workflows/release.yml"
$archive = "$variant-$tag-windows-amd64.zip"

gh attestation verify ./build-info.json --repo "$repository" --signer-workflow "$workflow" --source-ref "refs/tags/$tag"
if ($LASTEXITCODE -ne 0) { throw "Build information verification failed" }

$build = Get-Content ./build-info.json -Raw | ConvertFrom-Json
gh attestation verify "$archive" --repo "$repository" --signer-workflow "$workflow" --source-ref "refs/tags/$tag" --source-digest "$($build.source_commit)"
if ($LASTEXITCODE -ne 0) { throw "ZIP verification failed" }
```

Both commands must succeed. The second also checks that the ZIP's attestation names the same source commit as the verified build information. If verification fails, don't treat the files as verified or continue using their metadata as trusted instructions.

Earlier releases, including v0.1.2, predate this workflow and may have neither attestations nor build information. These commands cannot retroactively verify them.

### Verify the Windows installer

Download the setup executable and `build-info.json` from the same release. This verifies the installer without running it:

```powershell
$tag = "v0.1.3" # Replace with the release tag.
$repository = "cwg-rpg-community/uploader"
$workflow = "$repository/.github/workflows/release.yml"
$setup = "cwg-rpg-uploader-$tag-windows-amd64-setup.exe"

gh attestation verify ./build-info.json --repo "$repository" --signer-workflow "$workflow" --source-ref "refs/tags/$tag"
if ($LASTEXITCODE -ne 0) { throw "Build information verification failed" }
$build = Get-Content ./build-info.json -Raw | ConvertFrom-Json
gh attestation verify "$setup" --repo "$repository" --signer-workflow "$workflow" --source-ref "refs/tags/$tag" --source-digest "$($build.source_commit)"
if ($LASTEXITCODE -ne 0) { throw "Installer verification failed" }
```

For an independent Go payload rebuild, also obtain and verify the matching portable ZIP, then follow the steps below. Compare payload executables, not installer containers; installer output is not promised to be byte-reproducible. The compiler bootstrap is pinned in `scripts/install-inno.ps1`; the build information records the compiler version and hash.

## 2. Rebuild the executable

You'll need Windows, [Git](https://git-scm.com/), and the exact [Go version](https://go.dev/dl/) shown by:

```powershell
$build.go.GOVERSION
```

Use the verified build information from step 1, not a file from another release. Continue in the same PowerShell session so the variables remain available.

Extract the verified ZIP into a new folder and remember the downloaded executable's location:

```powershell
Expand-Archive -Path "$archive" -DestinationPath ./verified-download
$downloadedExecutable = (Resolve-Path "./verified-download/$variant.exe").Path
```

Clone the public repository into a new directory and check out the release tag:

```powershell
git clone https://github.com/cwg-rpg-community/uploader.git uploader-rebuild
if ($LASTEXITCODE -ne 0) { throw "Clone failed" }
cd uploader-rebuild
git checkout --detach "$tag"
if ($LASTEXITCODE -ne 0) { throw "Checkout failed" }
if ((git rev-parse HEAD) -ne $build.source_commit) { throw "Tag does not match the verified source commit" }
if (git status --porcelain) { throw "The checkout must be clean" }
```

Keep the `.git` directory. Go embeds the commit, commit timestamp, and clean/dirty state; building from GitHub's source ZIP omits that metadata and changes the executable's bytes. Don't edit the checkout before building.

These are the settings used by this version of the release workflow:

```powershell
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:GOAMD64 = "v1"
$env:CGO_ENABLED = "0"
$env:GOTOOLCHAIN = "local"
$env:GOFLAGS = ""
$env:GOEXPERIMENT = ""

if ((go env GOVERSION) -ne $build.go.GOVERSION) { throw "Install the exact Go version recorded in build-info.json" }

go build -trimpath -ldflags="-H=windowsgui" -o dist/cwg-rpg-uploader.exe .
if ($LASTEXITCODE -ne 0) { throw "Tray build failed" }
go build -tags=console -trimpath -o dist/cwg-rpg-uploader-console.exe .
if ($LASTEXITCODE -ne 0) { throw "Console build failed" }
```

Use the instructions at the release tag you're checking, not necessarily the default branch: build settings can change between releases. Older releases may differ; v0.1.2, for example, used `CGO_ENABLED=1` and stripped symbols with `-s -w`.

Compare the downloaded executable, your rebuilt executable, and the hash recorded in the verified build information:

```powershell
$downloadedHash = (Get-FileHash "$downloadedExecutable" -Algorithm SHA256).Hash
$rebuiltHash = (Get-FileHash "./dist/$variant.exe" -Algorithm SHA256).Hash
$recordedHash = ($build.executables | Where-Object { $_.name -eq "$variant.exe" }).sha256

if (-not $recordedHash -or $downloadedHash -ne $recordedHash -or $rebuiltHash -ne $downloadedHash) {
    throw "Executable hashes do not match; investigate before trusting the build"
}
"Match: $rebuiltHash"
```

A match means your independent build produced the exact executable distributed in that ZIP. No need to launch it. Compare the executables, not newly created ZIPs: archive timestamps and compression can differ.

## What the build information tells you

The attested `build-info.json` records the source commit, workflow run, Go settings, runner image version, pinned action revisions, executable hashes (including setup when present), and the Inno Setup compiler version/hash. It preserves those details beyond the lifetime of Actions logs. It does not let you request an old GitHub runner image or guarantee that every future environment can reproduce the build.

Attestations protect against substituting different files without the expected workflow identity. They don't protect against malicious source, compromised build tools, or someone authorized to change the trusted workflow. Rebuilding provides an additional source-to-binary check, not a code-security audit.
