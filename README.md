# qucs-mcp

MCP-сервер, который из SPICE-текста нетлиста собирает схему Qucs-S (`.sch`),
проверяет её реальным нетлистом через `qucs-s -n --ngspice` и запускает
симуляцию. Headless: qucs-s.exe нужен только как subprocess.

```
SPICE-текст → internal/spice → model → internal/layout → .sch
            → qucs-s -n --ngspice → .cir → internal/netlist → internal/validate
            → qucs-s -n --ngspice --run → .dat → internal/dataset
```

## Сборка

```sh
go build -o bin/qucs-mcp.exe ./cmd/qucs-mcp
```

Требуется Go 1.25+ и локальная установка Qucs-S (каталог `qucs/` рядом с
репозиторием, `QUCS_S_ROOT` или системный путь — см. `internal/config`).

## MCP-инструменты

| Инструмент | Назначение |
|---|---|
| `doctor` | проверка установки Qucs-S и конфигурации — звать первым |
| `create_schematic(netlist, name?, output_dir?, base_path?, validate=true, simulate=false, overwrite=false)` | SPICE-текст → `.sch`; возвращает `path`, `netlist_path`, `valid`, `report`, `warnings`, `simulation` |
| `update_schematic(path, netlist, validate, simulate)` | перегенерировать существующий `.sch` из нового SPICE-текста, сохранив позиции компонентов по ref |
| `validate_schematic(path, expected_netlist="")` | нетлист существующей схемы против её модели (или явно заданного SPICE-текста) |
| `simulate_schematic(path, timeout_s=60, include_data=false)` | симуляция; сводка датасета — имена переменных и число точек (`include_data=true` добавляет массивы) |
| `read_schematic(path)` | модель существующей схемы: компоненты (ref/type/value/x/y) + помеченные провода; неизвестные типы — предупреждения, не ошибки |
| `list_components()` | каталог поддерживаемых типов: R, C, L, S4Q_V, S4Q_I, DIODE_SPICE, _BJT, _MOSFET, GND, .AC, .TR, .DC, SpicePar, SpiceInclude |
| `describe_component(type)` | детальный вход каталога: пины со смещениями, ориентация, пример строки схемы |
| `open_schematic(path)` | открыть схему в GUI qucs-s отдельным процессом (не блокирует сервер) |

Ошибки инструментов возвращаются структурированно: SDK отдаёт результат
с `IsError=true`, сервер продолжает работать.

## Регистрация в opencode

`~/.config/opencode/opencode.json` (или `%USERPROFILE%\.config\opencode\opencode.json`):

```json
{
  "mcp": {
    "qucs": {
      "type": "local",
      "command": ["C:\\path\\to\\qucs-mcp\\bin\\qucs-mcp.exe"],
      "enabled": true,
      "environment": {
        "QUCS_S_ROOT": "C:\\path\\to\\Qucs-S-26.1.1-win64"
      }
    }
  }
}
```

## Переменные окружения

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `QUCS_S_ROOT` | локальный `./qucs`, затем системная установка | корень Qucs-S |
| `QUCS_MCP_WORKDIR` | `~/Documents/qucs-mcp` | куда писать схемы |
| `QUCS_MCP_TIMEOUT` | `60s` | таймаут subprocess qucs-s |

## Caveats (откалибровано)

- **Стоп-суффикс**: spice4qucs превращает Qucs `.AC` стоп `1M` в ngspice
  `1m` (мега тихо теряется, sweep укорачивается). Layout пишет start/stop
  суффикс-свободной научной формой (`1e+06`) — проверено на 10→1e6, 456 точек.
- В SPICE-тексте `1M` = **милли**; мега — `1MEG`/`10MEG`. Нормализация
  значений делает соответствующий перевод в конвенции Qucs.
- Связность задают **метки проводов**: одинаковые метки на физически
  раздельных проводах сливаются в одну цепь; раскладка не влияет на электрику.
- Провода соединяются с пином только концом провода, точно на точке пина
  (калибровка пинов: S4Q_V/I mirror=0 пин1=(+30,0) «+»; вертикальные
  пассивы Rot=3 пин1 сверху; _BJT: C,B,E; _MOSFET: D,G,S с bulk→S).

## Тесты

```sh
go test ./... -timeout 300s
```

Юнит-тесты работают без Qucs-S; интеграционные (симуляция, нетлист)
пропускаются, если `qucs-s.exe` не найден. Регрессия читалки прогоняется
по всем `.sch` из `qucs/share/qucs-s/examples` (248 файлов).
