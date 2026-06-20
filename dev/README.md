# Crate dev environment

1. Drop a few Creative-Commons audio files into `dev/music/` (e.g. tracks from
   https://freemusicarchive.org or the Navidrome demo set). They are gitignored
   except `.gitkeep`.
2. `docker compose -f docker-compose.dev.yml up` → Navidrome at http://localhost:4533
   (first run: create an admin user in the Navidrome UI).
3. Run Crate against it (M1 adds the Subsonic adapter):
   - `cd web && npm run dev`
   - `go run ./cmd/crate --dev`
   Open http://localhost:8090.
