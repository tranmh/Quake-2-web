# Legal notes

- Engine/game code: GPLv2 (derived from id Software's Quake II source release). This project is GPLv2.
- No game data is committed. Nothing derived from pak files (parsed lumps, textures, fixture outputs) is committed;
  only hashes (`fixtures/MANIFEST.sha256`) and synthetic fixtures made from scratch.
- The demo archive is downloaded unmodified from a mirror and verified by sha256 (`tools/demo.sha256`).
- Retail/CTF paks are user uploads; assets derived from them are served only to the uploading account
  (and to players who own a pak with identical content hashes).
