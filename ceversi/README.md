# Ceversi

> C-crafted Othello that anyone can spin up, feather-light, and unapologetically slick.

Ceversi is a minimalist, SSL-ready Reversi/Othello server written in straight-up C. It ships with a tiny deployment footprint, a dockerized distro, and zero-nonsense scripts so you can run games on your own turf without wrestling yet another web stack.

## Why You’ll Vibe
- Pure C core that stays lean and predictable.
- SQLite-backed stats so every flip counts.
- SSL out of the box; no mystery meat configs.
- One-touch deploy script for the “I just wanna play” crowd.

## Spin It Up
### Easiest path (recommended)
```bash
chmod +x quick-deploy.sh
./quick-deploy.sh
```
This handles key generation, dependency setup, Docker build, and boots the service at `https://localhost:31744` (self-signed cert, so allow it once).

### Bare-metal build
```bash
./setup_libs.sh      # install native deps once
make                 # builds ./server
./server             # launches the C backend
```
Feel free to customize `docker-compose.yml` or `Makefile` if you’re targeting something exotic.

## Project Map
- `src/` – the C engine (handlers, utils, DB glue).
- `public/` + `index.html` – the chill front-end.
- `quick-deploy.sh` – all-in-one bootstrap.
- `tests/` – harness for verifying move logic.

## Contribute Your Spice
1. Fork, branch, hack.
2. Keep it lightweight; comment only when future-you would be confused.
3. Send a PR with screenshots or logs if you tweaked visuals/networking.

Made with competitive energy and a can of Dr. Pepper. Have fun flipping.
