/** Raised for malformed / truncated file data (the C code would Com_Error or crash). */
export class FormatError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'FormatError';
  }
}
