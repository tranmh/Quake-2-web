# ADR 0002-glibc-rand-port

Status: accepted

The game uses rand(). We port glibc's TYPE_3 random() generator exactly in Go and TS, inject it per game instance, and keep every call site in the original order, making bit-exact comparison against the C oracle possible.
