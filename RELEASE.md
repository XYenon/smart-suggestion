# Release Guide

This document explains how to create releases for the Smart Suggestion project.

## GitHub Workflow

The project includes an automated release workflow (`.github/workflows/release.yml`) that:

1. **Builds binaries** for multiple platforms:
   - Linux (x86_64, ARM64)
   - macOS (Intel, Apple Silicon)
   - Windows (x86_64)

2. **Creates release packages** containing:
   - Platform-specific binary (`smart-suggestion`)
   - Plugin file (`smart-suggestion.plugin.zsh`)
   - Documentation (`README.md`)

3. **Publishes GitHub release** with:
   - All platform archives
   - Installation script
   - Release notes

## Creating a Release

### Method 1: Tag-based Release (Recommended)

1. Create and push a new tag:
```bash
git tag v1.0.0
git push origin v1.0.0
```

2. The workflow will automatically trigger and create the release.

### Method 2: Manual Release

1. Go to the GitHub Actions tab in your repository
2. Select the "Release" workflow
3. Click "Run workflow"
4. Enter the desired tag name (e.g., `v1.0.0`)
5. Click "Run workflow"

## Installation Script

The `install.sh` script provides automated installation with the following features:

### Features
- **Platform detection**: Automatically detects OS and architecture
- **Dependency checking**: Verifies prerequisites (zsh, download tools)
- **Automatic configuration**: Sets up `.zshrc` automatically
- **Backup creation**: Backs up existing configuration
- **Error handling**: Provides clear error messages and recovery instructions

### Usage Examples

**Basic installation:**
```bash
curl -fsSL https://raw.githubusercontent.com/XYenon/smart-suggestion/main/install.sh | bash
```

**Custom installation directory:**
```bash
INSTALL_DIR="$HOME/.local/share/smart-suggestion" curl -fsSL https://raw.githubusercontent.com/XYenon/smart-suggestion/main/install.sh | bash
```

**Uninstallation:**
```bash
curl -fsSL https://raw.githubusercontent.com/XYenon/smart-suggestion/main/install.sh | bash -s -- --uninstall
```

**Help:**
```bash
curl -fsSL https://raw.githubusercontent.com/XYenon/smart-suggestion/main/install.sh | bash -s -- --help
```

### What the installer does

1. **Checks prerequisites**: zsh, curl/wget, tar
2. **Detects platform**: OS and architecture
3. **Downloads release**: Latest compatible binary package
4. **Installs files**: Extracts to `~/.config/smart-suggestion`
5. **Configures shell**: Adds source line to `.zshrc`
6. **Verifies dependencies**: Checks for zsh-autosuggestions
7. **Provides guidance**: Shows next steps and configuration options

## Release Checklist

Before creating a release:

- [ ] Update version in documentation if needed
- [ ] Test the build script locally: `./build.sh`
- [ ] Test the plugin functionality
- [ ] Update CHANGELOG.md (if exists)
- [ ] Ensure all tests pass
- [ ] Create and test the release
- [ ] Verify installation script works with the new release

## Troubleshooting Releases

### Build Failures
- Check Go version compatibility (requires 1.21+)
- Verify all dependencies are available
- Check for platform-specific build issues

### Installation Issues
- Verify release assets are properly uploaded
- Test installation script with different platforms
- Check download URLs and permissions

### Platform Support
The current setup supports:
- **Linux**: x86_64, ARM64
- **macOS**: Intel (x86_64), Apple Silicon (ARM64)  
- **Windows**: x86_64

To add new platforms, update the matrix in `.github/workflows/release.yml`.
