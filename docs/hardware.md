# stone-access hardware integration

How an `access-controller` drives physical doors. The authorization **decision**
is a pure function that runs centrally and at the edge (`internal/policy`); this
page is only about the **edge box's I/O** — the relays it energizes and the door
inputs it reads. For the config keys that select hardware see
[`configuration.md`](configuration.md); for *why* the driver layer is shaped this
way see `CLAUDE.md`.

## At a glance

| Board | Model string | Transport | Relays | Inputs | Status |
|---|---|---|---|---|---|
| KinCony Server-Mini (Pi CM4) | `kincony-server-mini` | native GPIO char device | 8 | 8 | pin map verified; polarity bench-item |
| KinCony Pi5R8 (Pi CM5) | `kincony-pi5r8` | MCP23017 over I2C | 8 | 8 | topology from KinCony flow; polarity + 2nd expander bench-items |

Both run on Linux edge hardware only. The reader is **simulated** on both (see
[below](#the-reader-is-simulated-v1)).

## The model

**Logical, not physical, in policy.** A portal record carries *logical* 1-based
indices — `lockRelay`, `dpsInput`, `rexInput` — plus the `controller` that drives
it and its `model`. The controller resolves those indices to physical lines
through the model's **profile** (`internal/drivers/hardware`). So rewiring a door
to a different relay, or swapping the box for a spare, is a policy edit — the
controller's local config is just its identity (`controller.code`) and hardware
selection (`controller.driver` / `controller.model`). Logical index *N* maps to
the board's labelled **OUT*N*** / **IN*N***.

**Transport is chosen by the model, not config.** `controller.driver: mock`
(default) drives no I/O. `controller.driver: gpio` means "real hardware"; the
`model`'s `Profile.Transport()` then selects the backend — native GPIO
(`internal/drivers/gpio`) or MCP23017 I2C (`internal/drivers/i2c`). Neither the
binary nor the config differs per board.

**Polarity** is encoded per line in the profile, not in config:

- **Relays — active-high.** A logical "energize" drives the line/latch high
  (relay ON). The line boots and returns to low (de-energized).
- **Inputs — active-low with pull-up.** The isolated input asserts by pulling to
  GND; an open contact reads inactive. The driver enables a pull-up and treats
  the low state as "active" (GPIO `AsActiveLow`; MCP23017 `IPOL`).

**Fail-safe everywhere.** Relays initialize de-energized and de-energize on
disarm and on controller shutdown/crash (the door re-locks; egress stays
hardware-owned). An unknown model arms nothing; an undefined or unset (`0`)
relay/input index is rejected rather than driving a wrong line; an input read
error keeps the last known state. A pulse defaults to **5 s** when a portal sets
no value.

DPS (door-position) and REX (request-to-exit) inputs feed the controller's
per-door forced / held-open state machine; an optional auxiliary input/output is
observe/drive-only. The held-open threshold and the relay/input indices ride the
**portal record** in policy, never the pure decision.

## KinCony Server-Mini (CM4) — `kincony-server-mini`

Native GPIO: 8 relays and 8 isolated inputs wired directly to the CM4's
Broadcom GPIO. Driven over the Linux GPIO character device via `go-gpiocdev`
(**no cgo**). The chip is `gpiochip0` (the BCM2711 bank); line offset = BCM
number.

Relays (logical → BCM):

| Logical | Label | BCM | | Logical | Label | BCM |
|---|---|---|---|---|---|---|
| relay 1 | OUT1 | 5 | | relay 5 | OUT5 | 6 |
| relay 2 | OUT2 | 22 | | relay 6 | OUT6 | 13 |
| relay 3 | OUT3 | 17 | | relay 7 | OUT7 | 19 |
| relay 4 | OUT4 | 4 | | relay 8 | OUT8 | 26 |

Inputs (logical → BCM):

| Logical | Label | BCM | | Logical | Label | BCM |
|---|---|---|---|---|---|---|
| input 1 | IN1 | 18 | | input 5 | IN5 | 12 |
| input 2 | IN2 | 23 | | input 6 | IN6 | 16 |
| input 3 | IN3 | 24 | | input 7 | IN7 | 20 |
| input 4 | IN4 | 25 | | input 8 | IN8 | 21 |

The pin map is **verified against KinCony's published CM4 pin definition**. Relay
and input **polarity** follows the board's wiring convention — confirm on the
bench before production.

Other peripherals the board breaks out (not driven today, relevant to the future
reader): **RS485** on BCM 14/15 (`TXD0`/`RXD0`), **I2C-1** on BCM 2/3, a 433 MHz
receiver on BCM 27.

## KinCony Pi5R8 (CM5) — `kincony-pi5r8`

All 16 relay/input lines hang off a **single MCP23017 I2C expander at `0x20` on
bus 1 (`/dev/i2c-1`)** — the CM5's native GPIO is not used for door I/O. The bus
is driven by the pure-Go **periph.io** stack (no cgo). The MCP23017 has no
host-side edge delivery without wiring its INT line to a GPIO, so inputs are read
by **polling** (~50 ms): a single goroutine samples the input ports and emits an
event on each change.

The MCP23017's two 8-bit ports split the I/O — inputs on Port A, relays on Port B:

| Logical | Label | MCP23017 pin | Port |
|---|---|---|---|
| input 1–8 | IN1–IN8 | 0–7 | A (pull-up, active-low) |
| relay 1–8 | OUT1–OUT8 | 8–15 | B (active-high) |

So relay *N* = pin `7+N` and input *N* = pin `N-1`. Relays sharing Port B's output
latch are written through a cached shadow register, so driving one relay never
disturbs the others.

The topology (chip, address, bus, port split) is taken from KinCony's reference
Node-RED flow. Two items to confirm on the bench: relay **polarity** (assumed
active-high), and a **second MCP23017 at `0x22`** that appears in the flow but is
wired to nothing — likely an expansion variant; only `0x20` is modelled.

Other devices on the same bus (the driver claims only `0x20`): an ADS1115 ADC at
`0x48`, an SSD1306 OLED at `0x3C`. Serial for the future reader: **RS485** on
`/dev/ttyAMA2`, **RS232** on `/dev/ttyAMA0`.

## The reader is simulated (v1)

No physical credential reader is driven yet. Taps arrive over NATS by publishing
to `acc.{location}.{type}.{thing}.tap` (see [`protocol.md`](protocol.md)); the
controller decides locally and pulses the lock. A real OSDP/Wiegand `ReaderDriver`
slots in behind the `drivers.ReaderDriver` interface later without touching the
tap loop or the decision core — the RS485 ports noted on both boards are the
intended path.

## Adding a board

The driver layer is data-first: a new board that uses an **existing transport** is
just a new `Profile`.

1. Add a `Profile` to `internal/drivers/hardware` (`profile.go`): the `relays` and
   `inputs` maps, each line a `LineSpec` — `gpioRelay`/`gpioInput` for native GPIO,
   `i2cLine` for an expander. Keep logical index *N* = OUT*N* / IN*N*.
2. Add the model string to the `controllers.model` select in `pbmigrations` and to
   [`configuration.md`](configuration.md).
3. That's it for an existing transport — `Profile.Transport()` routes the new model
   to the matching backend automatically.

A board on a **new transport or expander chip** also needs a backend implementing
`controller.PortalHardware` + `controller.AuxHardware` + `drivers.DoorInput` +
`Close()` (the GPIO and I2C packages are the two worked examples), plus a case in
the controller's backend selection.

## Bench checklist

Before trusting a board in production, confirm on the bench:

- **Relay polarity** — energizing a logical relay actually closes the strike
  circuit (both boards assume active-high).
- **Input polarity / DPS sense** — the door-position contact's NO/NC orientation
  matches active-low (door-shut reads "closed").
- **Pi5R8 only** — whether the `0x22` second expander exists and carries any I/O,
  and that the ~50 ms poll latency is acceptable for REX/forced detection.
