# API coverage

Generated from `api/catalog.json`. KVMD route and parameter inventory is independently extracted in `api/upstream.json` from official KVMD commit `fe26d028fc493942e2f951f8cd487cd5163238e3`. All 15 inbound WebSocket handlers (including binary protocol layouts and helper implementations) are pinned by AST fingerprints. uStreamer HTTP operations are audited against `296639a2acd06f8f39131e2d27170caccf8fd63a`.

72 routes: 70 HTTP operations and 2 WebSocket upgrades. Each has a named library service method and CLI command.

| HTTP | Path | Command | Query/path parameters | Form/JSON fields |
| --- | --- | --- | --- | --- |
| GET | `/api/alerts` | `alerts state` |  |  |
| POST | `/api/alerts/ack` | `alerts ack` | `alerts*` |  |
| GET | `/api/atx` | `atx state` |  |  |
| POST | `/api/atx/click` | `atx click` | `button*`, `wait` |  |
| POST | `/api/atx/power` | `atx power` | `action*`, `wait` |  |
| GET | `/api/auth/check` | `auth check` |  |  |
| POST | `/api/auth/login` | `auth login` |  | `expire`, `passwd`, `redirect`, `user` |
| POST | `/api/auth/logout` | `auth logout` |  |  |
| GET | `/api/export/prometheus/metrics` | `system metrics` |  |  |
| GET | `/api/gpio` | `gpio state` |  |  |
| POST | `/api/gpio/pulse` | `gpio pulse` | `channel*`, `delay`, `wait` |  |
| POST | `/api/gpio/switch` | `gpio switch` | `channel*`, `state*`, `wait` |  |
| GET | `/api/hid` | `hid state` |  |  |
| POST | `/api/hid/events/send_key` | `hid key` | `finish`, `key*`, `state` |  |
| POST | `/api/hid/events/send_mouse_button` | `hid button` | `button*`, `state` |  |
| POST | `/api/hid/events/send_mouse_move` | `hid move` | `to_x*`, `to_y*` |  |
| POST | `/api/hid/events/send_mouse_relative` | `hid relative` | `delta_x*`, `delta_y*` |  |
| POST | `/api/hid/events/send_mouse_wheel` | `hid wheel` | `delta_x*`, `delta_y*` |  |
| POST | `/api/hid/events/send_shortcut` | `hid shortcut` | `keys*` |  |
| GET | `/api/hid/inactivity` | `hid inactivity` |  |  |
| GET | `/api/hid/keymaps` | `hid keymaps` |  |  |
| POST | `/api/hid/print` | `hid type` | `delay`, `keymap`, `limit`, `slow` |  |
| POST | `/api/hid/reset` | `hid reset` |  |  |
| POST | `/api/hid/set_connected` | `hid connect` | `connected*` |  |
| POST | `/api/hid/set_params` | `hid params` | `jiggler`, `keyboard_output`, `mouse_output` |  |
| GET | `/api/info` | `system info` | `fields`, `legacy` |  |
| GET | `/api/log` | `system log` | `follow`, `seek` |  |
| GET | `/api/media` | `media state` |  |  |
| GET | `/api/media/ws` | `media connect` | `video` |  |
| GET | `/api/msd` | `msd state` |  |  |
| GET | `/api/msd/read` | `msd download` | `compress`, `image*` |  |
| POST | `/api/msd/remove` | `msd remove` | `image*` |  |
| POST | `/api/msd/reset` | `msd reset` |  |  |
| POST | `/api/msd/set_connected` | `msd connect` | `connected*` |  |
| POST | `/api/msd/set_params` | `msd params` | `cdrom`, `guess`, `image`, `passwd`, `retries_delay`, `rw`, `timeout`, `user`, `verify` |  |
| POST | `/api/msd/write` | `msd upload` | `activate`, `image*`, `prefix`, `remove_incomplete` |  |
| POST | `/api/msd/write_remote` | `msd fetch` | `activate`, `image`, `insecure`, `prefix`, `remove_incomplete`, `timeout`, `url*`, `verify` |  |
| GET | `/api/streamer` | `streamer state` |  |  |
| GET | `/api/streamer/ocr` | `streamer ocr` |  |  |
| POST | `/api/streamer/reset` | `streamer reset` |  |  |
| POST | `/api/streamer/set_params` | `streamer params` | `desired_fps`, `h264_bitrate`, `h264_gop`, `quality`, `resolution` |  |
| DELETE | `/api/streamer/snapshot` | `streamer remove-snapshot` |  |  |
| GET | `/api/streamer/snapshot` | `streamer snapshot` | `allow_offline`, `load`, `ocr`, `ocr_bottom`, `ocr_langs`, `ocr_left`, `ocr_right`, `ocr_top`, `preview`, `preview_max_height`, `preview_max_width`, `preview_quality`, `save` |  |
| GET | `/api/switch` | `switch state` |  |  |
| POST | `/api/switch/atx/click` | `switch click` | `button*`, `port*` |  |
| POST | `/api/switch/atx/power` | `switch power` | `action*`, `port*` |  |
| POST | `/api/switch/edids/change` | `switch edid-change` | `data`, `id*`, `name` |  |
| POST | `/api/switch/edids/create` | `switch edid-create` | `data*`, `name*` |  |
| POST | `/api/switch/edids/remove` | `switch edid-remove` | `id*` |  |
| POST | `/api/switch/reset` | `switch reset` | `bootloader`, `unit*` |  |
| POST | `/api/switch/set_active` | `switch active` | `port*` |  |
| POST | `/api/switch/set_active_next` | `switch next` |  |  |
| POST | `/api/switch/set_active_prev` | `switch previous` |  |  |
| POST | `/api/switch/set_beacon` | `switch beacon` | `downlink`, `port`, `state*`, `uplink` |  |
| POST | `/api/switch/set_colors` | `switch colors` | `active`, `beacon`, `bootloader`, `flashing`, `inactive` |  |
| POST | `/api/switch/set_port_params` | `switch port` | `atx_click_power_delay`, `atx_click_power_long_delay`, `atx_click_reset_delay`, `dummy`, `edid_id`, `name`, `port*` |  |
| GET | `/api/ws` | `events connect` | `stream` |  |
| GET | `/redfish/v1` | `redfish root` |  |  |
| GET | `/redfish/v1/Managers` | `redfish managers` |  |  |
| GET | `/redfish/v1/Managers/BMC` | `redfish manager` |  |  |
| GET | `/redfish/v1/Managers/BMC/VirtualMedia` | `redfish virtual-media` |  |  |
| GET | `/redfish/v1/Managers/BMC/VirtualMedia/MSD` | `redfish media` |  |  |
| POST | `/redfish/v1/Managers/BMC/VirtualMedia/MSD/Actions/VirtualMedia.EjectMedia` | `redfish eject` |  |  |
| POST | `/redfish/v1/Managers/BMC/VirtualMedia/MSD/Actions/VirtualMedia.InsertMedia` | `redfish insert` |  | `Image`, `Inserted`, `Oem.PiKVM.DriveOptical`, `Password`, `UserName`, `VerifyCertificate`, `WriteProtected` |
| GET | `/redfish/v1/Systems` | `redfish systems` |  |  |
| GET | `/redfish/v1/Systems/{id}` | `redfish system` | `id` |  |
| PATCH | `/redfish/v1/Systems/{id}` | `redfish update` | `id` |  |
| POST | `/redfish/v1/Systems/{id}/Actions/ComputerSystem.Reset` | `redfish power` | `id` | `ResetType` |
| GET | `/streamer/state` | `video state` |  |  |
| GET | `/streamer/snapshot` | `video snapshot` |  |  |
| GET | `/streamer/stream` | `video mjpeg` | `key`, `extra_headers`, `advance_headers`, `dual_final_frames`, `zero_data` |  |
| GET | `/streamer` | `video compat` | `action`, `key`, `extra_headers`, `advance_headers`, `dual_final_frames`, `zero_data` |  |

`*` denotes required query parameters. Body fields can be supplied with the named flags shown by `--help` or through `--body`/`--input`. Login uses form fields; Redfish uses JSON. `id` selects the Redfish system path and defaults to `0`.

Both WebSocket commands accept every event through `--event-type`/`--event` and every binary opcode through `--binary-input`. `events` also has named ping, key, button, move, relative, wheel and binary subcommands. `media` has start, ping and keyframe subcommands.

Scope: the APIs served by main KVMD and KVMD-Media through PiKVM Nginx, plus uStreamer state/snapshot/MJPEG. Janus WebRTC signaling belongs to the separate Janus server and is outside the REST API inventory, as are optional third-party extras and internal Unix-socket daemons. Endpoint availability still depends on installed firmware and hardware. This is endpoint/parameter parity, not a claim of 100% code coverage or live hardware validation.

To audit a newer checkout, run `python3 scripts/audit-api.py /path/to/kvmd /path/to/ustreamer`. A difference fails with an inventory diff. Update the pinned inventory and catalog after review, then run `python3 scripts/generate-api.py` and `go test -race ./...`. Normal tests run offline against the pinned inventory.
