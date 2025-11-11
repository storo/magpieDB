#!/bin/bash
# MVCC Test Runner Script
# Quick reference for running MagpieDB MVCC tests

echo "MagpieDB MVCC Test Suite Runner"
echo "================================"
echo ""

case "${1:-help}" in
  all)
    echo "Running all MVCC tests..."
    go test -v -run "^TestMVCC" -timeout 5m
    ;;
  
  integration)
    echo "Running integration tests only..."
    go test -v -run TestMVCCFullLifecycle -run TestMVCCConcurrentReadersWriter \
      -run TestMVCCConflictDetection -run TestMVCCWithWALPersistence \
      -run TestMVCCVersionHistory -run TestMVCCIndexConsistency -timeout 2m
    ;;
  
  stress)
    echo "Running stress tests (may take time)..."
    go test -v -run TestMVCC100ConcurrentTransactions -run TestMVCCHighContention \
      -run TestMVCCMixedWorkload -timeout 10m
    ;;
  
  edge)
    echo "Running edge case tests..."
    go test -v -run TestMVCCNoPhantomReads -run TestMVCCNoLostUpdate \
      -run TestMVCCWriteSkew -run TestMVCCReadYourWrites -timeout 2m
    ;;
  
  bench)
    echo "Running benchmarks..."
    go test -bench=BenchmarkMVCC -benchmem -benchtime=10s
    ;;
  
  race)
    echo "Running with race detector (slow but thorough)..."
    go test -race -v -run "^TestMVCC" -timeout 15m
    ;;
  
  quick)
    echo "Running quick smoke tests..."
    go test -v -run TestMVCCFullLifecycle -run TestMVCCConflictDetection -timeout 30s
    ;;
  
  count)
    echo "Test Statistics:"
    echo "----------------"
    echo -n "Integration tests: "
    grep -c "^func Test" mvcc_integration_test.go
    echo -n "Stress tests: "
    grep -c "^func Test" mvcc_stress_test.go
    echo -n "Edge case tests: "
    grep -c "^func Test" mvcc_edge_cases_test.go
    echo -n "Benchmarks: "
    grep -c "^func Benchmark" mvcc_stress_test.go
    echo ""
    echo -n "Total lines: "
    wc -l mvcc_*.go | tail -1 | awk '{print $1}'
    ;;
  
  help|*)
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  all         - Run all MVCC tests (5 min timeout)"
    echo "  integration - Run integration tests only"
    echo "  stress      - Run stress tests (10 min timeout)"
    echo "  edge        - Run edge case tests"
    echo "  bench       - Run performance benchmarks"
    echo "  race        - Run with race detector (15 min timeout)"
    echo "  quick       - Run quick smoke tests (30 sec)"
    echo "  count       - Show test statistics"
    echo "  help        - Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 all          # Run everything"
    echo "  $0 quick        # Quick validation"
    echo "  $0 bench        # Performance testing"
    echo ""
    ;;
esac
