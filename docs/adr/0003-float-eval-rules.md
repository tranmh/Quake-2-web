# ADR 0003-float-eval-rules

Status: accepted

C float storage with double promotion for double literals and libm calls. Ports mirror the C type of every expression (see PORTING.md). The oracle is compiled 64-bit with SSE, -ffp-contract=off, no fast-math.
