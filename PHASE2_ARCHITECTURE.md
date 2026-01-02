# Fase 2 — Arquitectura base en Go (SweepFS)

## 1) Estructura de carpetas y módulos
1. `cmd/sweepfs/`
   - Responsabilidad: entrypoint CLI, parseo de flags, bootstrap del runtime.
   - Puede importar: `internal/app`, `internal/config`.
   - No debe importar: `internal/ui` directamente (solo vía `app`).
2. `internal/app/`
   - Responsabilidad: orquestación de componentes, wiring de dependencias.
   - Puede importar: `internal/config`, `internal/domain`, `internal/services`, `internal/ui`.
   - No debe importar: nada de `cmd/`.
3. `internal/ui/`
   - Responsabilidad: TUI (modelo, update, render), mapeo de eventos a comandos.
   - Puede importar: `internal/domain`, `internal/state`.
   - No debe importar: `internal/services` directamente (solo vía interfaces).
4. `internal/domain/`
   - Responsabilidad: modelos de negocio (nodos, tamaños, tipos).
   - Puede importar: stdlib únicamente.
   - No debe importar: `internal/ui`, `internal/services`.
5. `internal/services/`
   - Responsabilidad: escaneo FS, acciones (copy/move/delete), cache.
   - Puede importar: `internal/domain`.
   - No debe importar: `internal/state`, `internal/ui`.
6. `internal/state/`
   - Responsabilidad: estado de sesión (selecciones, ruta actual, prefs).
   - Puede importar: `internal/domain`.
   - No debe importar: `internal/services`.
7. `internal/config/`
   - Responsabilidad: defaults, flags y carga de config opcional.
   - Puede importar: stdlib.
   - No debe importar: `internal/ui`, `internal/services`.

## 2) Selección de librerías TUI
1. **Framework principal**: `bubbletea`
   - Encaja con SweepFS: loop predecible, estado explícito, fácil test.
   - Resuelve: manejo de teclado, render por diff, componentes.
2. **Estilos**: `lipgloss`
   - Resuelve: temas, colores, layout limpio sin acoplar lógica.
3. **Inputs/teclas**: `bubbles/key`
   - Resuelve: mapeo consistente, documentación de atajos.

**Qué NO usar**
1. Frameworks con UI imperativa (complican test).
2. Dependencias para filesystem directo en UI (evitar acoplamiento).

## 3) Modelo de datos (núcleo)
```go
type NodeType int

const (
    NodeFile NodeType = iota
    NodeDir
)

type Node struct {
    ID           string
    Name         string
    Path         string
    Type         NodeType
    SizeBytes    int64
    AccumBytes   int64
    ModTime      time.Time
    ParentID     string
    ChildrenIDs  []string
    ChildCount   int
    Scanned      bool
}

type TreeIndex struct {
    Nodes map[string]*Node
    RootID string
}
```

**Consideraciones de performance**
1. `AccumBytes` cacheado por carpeta tras escaneo inicial.
2. `Scanned` permite lazy load por expansión.
3. `ChildrenIDs` evita cargar nodos no expandibles.
4. `TreeIndex` permite acceso O(1) por ID.

## 4) Sistema de eventos y navegación
1. **Flujo de eventos**
   - Tecla → `UI Event` → `Command` → `State Update` → `Render`.
2. **Separación input/update/render**
   - `ui.Model` contiene solo estado de vista.
   - `services` expone interfaces para acciones.
3. **Navegación**
   - Cursor por índice visible.
   - Selección múltiple con set de IDs.
   - Expansión por toggle de `Scanned` + `ChildrenIDs`.

**Loop (Bubble Tea)**
1. `Update()` recibe eventos (tecla, resultado de servicio).
2. `Update()` emite `Command` asíncronos (scan/copy/move).
3. Servicios responden con `Msg` sin tocar UI directamente.

**Evitar acoplamiento UI-FS**
1. UI llama a interfaces (`Scanner`, `Actions`).
2. `services` devuelve resultados con IDs y errores.
3. Estado de filesystem vive en `domain`; `state` solo para UI/app.

## 5) Configuración inicial y flags
1. **Flags (CLI)**
   - `--path` (ruta inicial, default `.`)
   - `--show-hidden` (bool)
   - `--safe-mode` (bloquea rutas críticas, default true)
2. **Config file (opcional, no MVP)**
   - Tema, atajos custom, filtros persistentes.
3. **Defaults**
   - Orden: tamaño.
   - Vista: árbol.
   - Modo seguro: activo.

**Futuro**
1. Persistencia de preferencias.
2. Perfiles por entorno.

## Diagrama de dependencias
```
cmd/sweepfs
  -> internal/app
      -> internal/ui
          -> internal/state
              -> internal/domain
      -> internal/services
          -> internal/domain
      -> internal/config
```

## Checklist de cierre Fase 2
1. Estructura de carpetas creada y documentada.
2. Interfaces entre `ui` y `services` definidas.
3. Modelos `Node` y `TreeIndex` definidos y estables.
4. Loop de eventos especificado con mensajes y comandos.
5. Flags básicos definidos con defaults.
