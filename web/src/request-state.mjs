// A role switch invalidates every pending read and mutation result.
export class RequestEpoch {
  epoch = 0;
  next() {
    return ++this.epoch;
  }
  current(ticket) {
    return ticket === this.epoch;
  }
}
// Retain a frozen payload/key after an uncertain network result.
export class Submission {
  /** @type {{payload: unknown, key: string} | null} */
  pending = null;
  /** @param {unknown} payload @param {string} key @returns {{payload: unknown, key: string}} */
  prepare(payload, key) {
    if (this.pending) return this.pending;
    const next = { payload: JSON.parse(JSON.stringify(payload)), key };
    this.pending = next;
    return next;
  }
  clear() {
    this.pending = null;
  }
}
