# GitHub Actions Workflows

Simple automation for k9s-deck: **automatic releases when you push to main**.

## 🚀 Workflows

### 1. **Release** (`release.yml`)
**Automatic release on main push** - The only workflow you need.

**Trigger:** Any push to `main` branch

**What it does:**
1. ✅ Analyzes commit message to determine version bump
2. ✅ Calculates new version automatically  
3. ✅ Creates git tag
4. ✅ Runs GoReleaser to publish release
5. ✅ Uploads binaries to GitHub Releases

**Version Detection:**
- **Major (v1.0.0 → v2.0.0)**: Commit contains "breaking", "major", or "BREAKING CHANGE"
- **Minor (v1.0.0 → v1.1.0)**: Commit contains "feat" or "feature" 
- **Patch (v1.0.0 → v1.0.1)**: Everything else (fixes, docs, etc.)

---

### 2. **Build and Test** (`build-and-test.yml`) 
**Quality gate** for all changes.

**Trigger:** Push/PR to main

**Features:**
- ✅ Go testing and building
- ✅ Cross-platform builds (Linux/macOS/Windows)
- ✅ Code linting with golangci-lint
- ✅ Security scanning with Gosec

## 🎯 Simple Workflow

### **Normal Development:**
```
1. Create feature branch
2. Work on feature  
3. Create PR to main
4. Merge PR → Automatic release! 🎉
```

### **Controlling Version Bumps:**
Just use the right words in your commit messages:

```bash
# Patch release (v1.0.0 → v1.0.1)
git commit -m "fix: resolve LSP autocomplete bug"

# Minor release (v1.0.0 → v1.1.0) 
git commit -m "feat: add LSP autocomplete functionality"

# Major release (v1.0.0 → v2.0.0)
git commit -m "feat: complete rewrite with breaking changes

BREAKING CHANGE: API changed completely"
```

## 🔧 Setup

**Required (automatically provided):**
- `GITHUB_TOKEN` - For creating releases

**Required Permissions:**
- Contents: write (for releases)
- Actions: read (for workflows)

**Dependencies:**
- GoReleaser (handled automatically)

## 📋 Examples

### **Adding a new feature:**
```bash
git checkout -b feature/amazing-feature
# ... work on feature ...
git commit -m "feat: implement amazing feature"
git push origin feature/amazing-feature
# Create PR and merge → Minor release (v1.1.0)
```

### **Fixing a bug:**
```bash  
git checkout -b fix/critical-bug
# ... fix bug ...
git commit -m "fix: resolve critical issue"
git push origin fix/critical-bug  
# Create PR and merge → Patch release (v1.0.1)
```

### **Breaking change:**
```bash
git checkout -b refactor/api-change
# ... make breaking changes ...
git commit -m "refactor: redesign API

BREAKING CHANGE: All method signatures changed"
git push origin refactor/api-change
# Create PR and merge → Major release (v2.0.0)
```

## 🎉 That's It!

**Simple rule:** Merge to main = automatic release with proper versioning.

No manual workflows, no complex triggers, no extra steps. Just:
1. **Commit with descriptive message**
2. **Merge to main** 
3. **Release happens automatically** 🚀

The release includes:
- Cross-platform binaries
- GitHub release with notes
- Proper semantic versioning
- Automatic changelog generation