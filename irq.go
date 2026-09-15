package tiny64

// irqState separates the Phi2 pin sample from acceptance at an instruction
// poll. An accepted request survives pin release until entry or NMI arbitration.
// With neither a sampled nor held candidate, a completing cycle only needs
// to update sampled; callers can skip poll classification and preserve pending.
// https://www.nesdev.org/wiki/Visual6502wiki/6502_Interrupt_Recognition_Stages_and_Tolerances
type irqState struct {
	sampled bool
	pending bool
	held    bool
}

func (q *irqState) clock(asserted bool, i uint8, poll, stalled bool) {
	eligible := q.sampled && i == 0
	if stalled {
		// The sequencer is held, not the IRQ synchronizer. Accumulate
		// candidates until the resumed microcode identifies whether this
		// was a poll cycle; discard them if it was not.
		q.held = q.held || eligible
	} else {
		q.pending = q.pending || poll && (q.held || eligible)
		q.held = false
	}
	// Bus reads/writes may acknowledge the source during this Phi2.
	q.sampled = asserted
}

func irqPoll(opcode, before, after uint8) bool {
	if before == 0 || opcode == 0 {
		return false // Opcode fetch and interrupt entry do not poll.
	}
	if opcode&0x1F == 0x10 {
		// All branches poll at operand fetch. Only crossing branches
		// reach TState 3; same-page completion at TState 2 is not a poll.
		return before == 1 || before == 3
	}
	return after == 0
}
