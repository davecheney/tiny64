// Independent replay using hash-pinned public Visual6502 sources.
// No dependency on tiny64 or on previously generated traces.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fixturePath = path.resolve(process.argv[2] ?? path.join(__dirname, 'irq-rdy-cycles.json'));
const sourceDir = path.resolve(process.argv[3] ?? path.join(__dirname, 'upstream'));
const fixture = JSON.parse(fs.readFileSync(fixturePath));
assert.equal(fixture.schema, 'nmos6502-irq-rdy-cycles-v1');
const source = Object.fromEntries(fixture.reference.files.map(f =>
  [f, fs.readFileSync(path.join(sourceDir, f), 'utf8')]));
const hashes = Object.fromEntries(Object.entries(source).map(([f, s]) =>
  [f, crypto.createHash('sha256').update(s).digest('hex')]));
assert.deepEqual(hashes, fixture.reference.sha256, 'public source files must match pinned reference');
const results = [];
for (const item of fixture.cases) {
  const s = {...fixture.defaults, ...item};
  const c = vm.createContext({console});
  for (const f of fixture.reference.files) vm.runInContext(source[f], c, {filename: f});
  vm.runInContext('refresh=function(){}; chipStatus=function(){}; setCellValue=function(){}; now=Date.now;', c);
  c.setupNodes();
  c.setupTransistors();
  c.memory = Array(65536).fill(s.memoryFill);
  const patch = (address, bytes) => bytes.forEach((b, i) => {c.memory[address + i] = b;});
  // Establish initial conditions with instructions, never by forcing CPU nodes.
  patch(0x0200, [0xa2, s.initialX, 0x9a, s.initialI ? 0x78 : 0x58,
    0xa9, s.initialA, 0xea, 0x4c, s.programAddress & 255, s.programAddress >> 8]);
  for (const p of [...fixture.defaults.memory, ...(item.memory ?? [])]) patch(p.address, p.bytes);
  patch(s.programAddress, s.program);
  patch(0xfffc, [0, 2]);
  patch(0xfffe, [s.irqVector & 255, s.irqVector >> 8]);
  patch(0xfffa, [0, 0x90]);
  patch(s.irqVector, s.handler);
  c.initChip();
  let entered = false;
  for (let n = 0; n < 300; n++) {
    if (c.readBit('cp1') && c.readBit('sync') && c.readAddressBus() === s.programAddress) {
      entered = true;
      break;
    }
    c.halfStep();
  }
  assert(entered, item.name + ': main opcode fetch not reached');
  assert.equal(c.readBit('p2'), s.initialI, item.name + ': initial I');
  assert.equal(c.readBit('p1'), s.initialZ, item.name + ': initial Z');
  assert.equal(c.readSP(), s.initialS, item.name + ': initial S');
  assert.equal(c.readBit('IRQP'), 0);
  assert.equal(c.readBit('INTG'), 0);
  const trace = [], fetches = [], writes = [];
  let previousSync = false, activeFetch;
  const lastWanted = (s.expected.secondFollowingFetch ?? s.expected.firstFollowingFetch).completionCycle;
  for (let cycle = 0; cycle <= lastWanted + 16; cycle++) {
    (s.irqAssertedCycles.includes(cycle) ? c.setLow : c.setHigh)('irq');
    const held = cycle >= s.readHold.startCycle && cycle < s.readHold.startCycle + s.readHold.length;
    (held ? c.setLow : c.setHigh)('rdy');
    c.halfStep();
    assert.equal(c.readBit('cclk'), 1);
    const row = {cycle, irqAsserted: !c.readBit('irq'), rdy: c.readBit('rdy'),
      address: c.readAddressBus(), data: c.readDataBus(), sync: c.readBit('sync'),
      read: c.readBit('rw'), IInPhi2: c.readBit('p2'), IRQP: c.readBit('IRQP'),
      INTG: c.readBit('INTG'), ir: c.readBits('ir', 8),
      states: c.allTCStates(false)};
    if (!row.read) writes.push({cycle, address: row.address, data: row.data});
    if (row.sync && !previousSync) {
      activeFetch = {cycle, address: row.address};
      fetches.push(activeFetch);
    }
    previousSync = !!row.sync;
    c.halfStep();
    row.IAfterCycle = c.readBit('p2');
    if (row.sync && !c.readBit('sync')) {
      activeFetch.completionCycle = cycle;
      activeFetch.irq = !!row.INTG;
      assert.equal(c.readBits('ir', 8), activeFetch.irq ? 0 : row.data,
        `${item.name}/${cycle}: fetched opcode or IRQ substitution`);
    }
    if (s.expected.iAfterCycle && cycle < s.expected.iAfterCycle.length) {
      assert.equal(row.IAfterCycle, s.expected.iAfterCycle[cycle], `${item.name}/${cycle}: architectural I`);
    }
    trace.push(row);
  }
  const wanted = [s.expected.firstFollowingFetch, s.expected.secondFollowingFetch].filter(Boolean);
  wanted.forEach((f, i) => assert.deepEqual(fetches[i + 1], f, `${item.name}: following fetch ${i + 1}`));
  if ('opcodeFetchIRQ' in s.expected) assert.equal(fetches[0].irq, s.expected.opcodeFetchIRQ);
  const expectsEntry = wanted.some(f => f.irq);
  if (expectsEntry) {
    assert(trace.some(r => r.read && r.address === 0xfffe), item.name + ': IRQ vector low');
    assert(trace.some(r => r.read && r.address === 0xffff), item.name + ': IRQ vector high');
    assert(fetches.some(f => f.address === s.irqVector), item.name + ': handler fetch');
    assert.equal(writes.length, 3, item.name + ': IRQ stack writes');
    assert.equal(writes[2].data & 0x10, 0, item.name + ': pushed B clear');
  } else {
    assert(!trace.some(r => r.INTG), item.name + ': no delayed acceptance in observation window');
  }
  results.push({name: item.name, fetches, writes, trace});
}
if (process.argv[4]) {
  fs.writeFileSync(process.argv[4], JSON.stringify({revision: fixture.reference.revision,
    node: process.version, hashes, cases: results.length, results}, null, 2) + '\n');
}
console.log(`PASS: ${results.length} portable cycle schedules; fetch substitution, entry, and I boundaries verified.`);
