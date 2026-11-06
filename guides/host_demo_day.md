# Demo day: host the network from the Mac

One terminal starts everything. Guests join by scanning a QR code. No ZeroTier.

## Once, before the day

```bash
brew install cloudflared bore-cli      # the two free tunnels (no account)
git fetch && git switch vyomanaut_open_network && git pull
```

Copy the `dist/` folder from the Windows laptop into the repo on the Mac (AirDrop or USB). It
holds the Android app and the laptop providers that the join page hands out. Without it, the
downloads come from the GitHub release, which must then carry the **new** app (it opens from
the QR link; the old one does not).

## Start the network (terminal 1)

```bash
cd Vyomanaut_For_ETH

# Same RocksDB exports as always (optional: without them the pure-Go engine is used)
export CGO_CFLAGS="-I$HOME/rocksdb/include"
export CGO_LDFLAGS="-L$HOME/rocksdb/lib -L$(brew --prefix)/lib -L$(brew --prefix snappy)/lib \
  -Wl,-rpath,$HOME/rocksdb/lib -Wl,-rpath,$(brew --prefix)/lib -Wl,-rpath,$(brew --prefix snappy)/lib \
  -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd"
export DYLD_LIBRARY_PATH="$HOME/rocksdb/lib:$(brew --prefix)/lib:$(brew --prefix snappy)/lib:$DYLD_LIBRARY_PATH"

# Fresh database (optional, same as before)
docker compose -f deployments/dev/docker-compose.yml down -v

# Chains, same as before
eval "$(scripts/demo/chain_env.sh)"
eval "$(scripts/demo/arkiv_env.sh)"

# Everything else: Postgres, relay, tunnels, coordinator, 5 providers, QR screen, join desk
scripts/demo/host.sh --name "Vyomanaut" --providers 5
```

No `MY_IP`, no `zerotier-cli`, no `down.sh` first: `host.sh` stops any earlier run itself.
When it finishes, two browser tabs open:

- **QR screen**: put it on the projector.
- **Join desk**: keep it on the Mac. Codes appear here as people register; read them out.

Leave terminal 1 alone. **Do not restart host.sh during the demo**: the tunnel addresses change
and the QR on screen stops working.

## Watch it (terminal 2), unchanged

```bash
source /tmp/vyomanaut-demo/env
"$BIN_DIR/operator" watch --mode=demo --microservice-url="$MICROSERVICE_URL" --admin-api-key="$ADMIN_API_KEY"
```

`operator otp` still works as a backup to the join desk:
`"$BIN_DIR/operator" otp --mode=demo --otp-delivery-log="$OTP_LOG" "+91XXXXXXXXXX"`.

## Stop

```bash
scripts/demo/host.sh --stop
```

## If the tunnels fail

Hall Wi-Fi sometimes blocks them. Fall back to the old ZeroTier way with the same pages:

```bash
scripts/demo/host.sh --name "Vyomanaut" --providers 5 --local --advertise-host "$MY_IP"
```

(`MY_IP` from `zerotier-cli` as before.) Guests on ZeroTier or the same Wi-Fi open
`http://$MY_IP:8080/join`.
