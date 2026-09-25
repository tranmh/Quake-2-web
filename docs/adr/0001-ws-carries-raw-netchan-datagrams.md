# ADR 0001-ws-carries-raw-netchan-datagrams

Status: accepted

One WebSocket binary message carries exactly one original UDP datagram, including the 10-byte netchan header and out-of-band (-1) packets. Keeps client and server netchan code faithful and lets a trivial WS<->UDP bridge connect the TS client to the original C dedicated server for differential tests.
