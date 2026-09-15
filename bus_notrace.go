//go:build tinygo || nobustrace

package tiny64

// BusTrace is false here, which drops the Address and RW stores from
// Bus.Load/Store and DriveBus.Load/Store.
//
// Those two lines are diagnostics: no part of the emulator ever reads them
// back, only the tests and cmd/c64cli's tracer do. But they are written on
// every CPU bus access - around 18,500 times per PAL frame, twice that with
// a 1541 attached - so on the RP2040 firmware targets they are 1.8 million
// stores a second that nothing observes.
//
// Measured, TinyGo 0.42 already removes them from the gopher-badge and
// tufty2040 firmware: the ELF is byte-identical with and without this file
// taking effect. LLVM can see the whole program, sees that nothing ever
// loads bus.Address or bus.RW, and deletes the stores. What it cannot see is
// the future: that elimination holds only while nothing in the firmware
// calls GetBus. Wire up a serial monitor or a crash dumper that does, and
// the 18,500 stores per frame come back, silently, with no source change to
// blame. This constant makes "these lines are not maintained in firmware" a
// property of the source rather than a side effect of reachability. The gc
// toolchain does not do the same elimination - it costs 16 bytes each in
// (*CPU).load and (*CPU).store - which is what makes the effect visible to
// the benchmarks at all.
//
// The `tinygo` half of the build constraint means the firmware gets the
// cheap path automatically, with no flag for anyone to forget. The
// `nobustrace` half exists so the same configuration can be built, vetted,
// tested and benchmarked by the ordinary Go toolchain - both in CI and on a
// desktop - because a configuration only the firmware toolchain can produce
// is a configuration nobody checks.
//
// If you are tempted to delete the `if BusTrace` guards because they look
// like dead weight: they are the only thing stopping a future GetBus caller
// in the firmware from reinstating 18,500 unobserved stores per frame.
const BusTrace = false
