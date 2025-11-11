# Contributing to MagpieDB

First off, thank you for considering contributing to MagpieDB! It's people like you that make MagpieDB a great tool.

## Code of Conduct

This project and everyone participating in it is governed by our Code of Conduct. By participating, you are expected to uphold this code.

## How Can I Contribute?

### Reporting Bugs

Before creating bug reports, please check the issue list as you might find out that you don't need to create one. When you are creating a bug report, please include as many details as possible:

* **Use a clear and descriptive title**
* **Describe the exact steps to reproduce the problem**
* **Provide specific examples**
* **Describe the behavior you observed and what you expected**
* **Include Go version, OS, and MagpieDB version**

### Suggesting Enhancements

Enhancement suggestions are tracked as GitHub issues. When creating an enhancement suggestion, please include:

* **Use a clear and descriptive title**
* **Provide a detailed description of the suggested enhancement**
* **Explain why this enhancement would be useful**
* **List any alternative solutions you've considered**

### Pull Requests

* Fill in the pull request template
* Follow the Go coding style (use `gofmt`)
* Include tests for new functionality
* Update documentation as needed
* End all files with a newline
* Ensure CI passes

## Development Setup

1. **Clone the repository**
   ```bash
   git clone https://github.com/voidlab/magpiedb.git
   cd magpiedb
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Run tests**
   ```bash
   make test
   ```

4. **Run linter**
   ```bash
   make lint
   ```

## Development Workflow

1. **Fork the repo** and create your branch from `main`
2. **Make your changes** and add tests
3. **Run the test suite**
   ```bash
   make test
   make test-race  # Check for race conditions
   ```
4. **Run benchmarks** if touching performance-critical code
   ```bash
   make bench
   ```
5. **Ensure code quality**
   ```bash
   make lint
   make fmt
   ```
6. **Commit your changes** with a descriptive message
7. **Push to your fork** and submit a pull request

## Coding Standards

### Go Style Guide

* Follow the [Effective Go](https://golang.org/doc/effective_go.html) guidelines
* Use `gofmt` for formatting
* Run `go vet` to catch common mistakes
* Add comments for exported functions and types
* Keep functions small and focused

### Testing

* Write tests for all new functionality
* Aim for >90% code coverage
* Use table-driven tests where appropriate
* Include benchmarks for performance-critical code

Example test structure:
```go
func TestFeature(t *testing.T) {
    tests := []struct {
        name     string
        input    interface{}
        expected interface{}
    }{
        {"case 1", input1, expected1},
        {"case 2", input2, expected2},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := Feature(tt.input)
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
```

### Documentation

* Add godoc comments for all exported types and functions
* Update README.md for user-facing changes
* Update ARCHITECTURE.md for design changes
* Include code examples in documentation

### Commit Messages

* Use the present tense ("Add feature" not "Added feature")
* Use the imperative mood ("Move cursor to..." not "Moves cursor to...")
* Limit the first line to 72 characters
* Reference issues and pull requests liberally

Example:
```
Add cosine similarity optimization

- Implement SIMD-friendly algorithm
- Add benchmarks showing 2x speedup
- Update documentation

Fixes #123
```

## Project Structure

```
magpieDB/
├── magpie.go           # Main API
├── types.go            # Core types
├── storage.go          # Storage engine
├── index.go            # HNSW index
├── distance.go         # Distance metrics
├── wal.go             # Write-ahead log
├── transaction.go     # Transaction support
├── filter.go          # Metadata filtering
├── *_test.go          # Tests
├── cmd/magpie/        # CLI tool
├── examples/          # Usage examples
└── docs/              # Documentation
```

## Running Tests

```bash
# All tests
make test

# With coverage
make test-coverage

# With race detector
make test-race

# Specific package
go test -v ./path/to/package

# Specific test
go test -v -run TestName
```

## Running Benchmarks

```bash
# All benchmarks
make bench

# Specific benchmark
go test -bench=BenchmarkName -benchmem

# Compare benchmarks
make bench > old.txt
# Make changes
make bench > new.txt
benchstat old.txt new.txt
```

## Adding New Features

When adding a new feature, please:

1. **Discuss first** - Open an issue to discuss the feature
2. **Follow the roadmap** - Check if it aligns with project goals
3. **Keep it simple** - MagpieDB values simplicity
4. **Add tests** - Comprehensive test coverage required
5. **Update docs** - User-facing changes need documentation
6. **Consider backward compatibility** - Avoid breaking changes

## Performance Considerations

MagpieDB is designed for performance. When contributing:

* **Benchmark your changes** - Prove performance improvements
* **Avoid allocations** in hot paths
* **Use `sync.Pool`** for frequently allocated objects
* **Profile** before optimizing
* **Document trade-offs** in comments

## Release Process

Releases are handled by maintainers:

1. Update version in `magpie.go`
2. Update CHANGELOG.md
3. Tag release: `git tag -a v1.2.3 -m "Release v1.2.3"`
4. Push tag: `git push origin v1.2.3`
5. GitHub Actions builds and releases automatically

## Getting Help

* **Documentation**: Start with README.md and docs/
* **Issues**: Search existing issues first
* **Discussions**: Use GitHub Discussions for questions
* **Discord**: Join our community (link in README)

## Recognition

Contributors are recognized in:
* README.md contributors section
* Release notes
* GitHub contributors page

Thank you for contributing! 🎉

---

**Questions?** Open an issue or start a discussion.
