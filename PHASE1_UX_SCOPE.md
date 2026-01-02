# Fase 1 — UX y Alcance (SweepFS)

## 1) Acciones mínimas del MVP
1. **Navegación**
   - Entrar/salir de carpetas.
   - Expandir/colapsar carpetas en árbol.
2. **Visualización**
   - Mostrar tamaño acumulado por carpeta.
   - Mostrar tamaño por archivo.
3. **Gestión**
   - Borrar archivos y carpetas.
   - Mover archivos y carpetas.
   - Copiar archivos y carpetas.
4. **Backups simples**
   - Copia manual a ruta destino en carpeta con timestamp (no incremental, no compresión).

**Fuera del MVP**
1. Sin sincronización automática ni incremental.
2. Sin compresión, cifrado ni versionado de backups.
3. Sin integración con nube.
4. Sin vistas gráficas avanzadas (charts, heatmaps).

**Diferencias archivo vs carpeta**
1. **Archivo**: acciones directas sobre el fichero.
2. **Carpeta**: acciones afectan recursivamente al contenido.
3. Confirmaciones distintas (carpeta requiere confirmación explícita por recursividad).

## 2) Flujos de interacción
1. **Navegación básica**
   - `↑/↓`: mover selección.
   - `Enter`: si hay hijos, expande/colapsa; si no, entra.
   - `Backspace`: salir al padre o colapsar.
2. **Selección**
   - Simple: foco actual.
   - Múltiple: marcar con `Space`.
   - `A`: seleccionar todo en vista actual.
   - `Esc`: limpiar selección.
3. **Acciones críticas**
   - `D`: borrar.
   - `M`: mover.
   - `C`: copiar.
   - `B`: backup manual.
4. **Confirmaciones**
   - **Borrar archivo**: confirmar con `y/n`.
   - **Borrar carpeta**: doble confirmación (“borrar recursivo”).
   - **Mover/Copiar**: pedir ruta destino + resumen pre-confirmación.
5. **Errores**
   - Permisos: mensaje no bloqueante en barra de estado.
   - Archivos en uso: aviso y salto al siguiente.
   - Acciones parciales: resumen final con fallos.

**Ejemplos**
1. Usuario selecciona carpeta → pulsa `D` → confirmación doble → carpeta eliminada.
2. Usuario marca varios archivos → pulsa `M` → introduce destino → confirma → mueve.
3. Usuario selecciona carpeta → pulsa `B` → elige ruta → copia completa.

## 3) Vista inicial y navegación
1. **Vista inicial**: árbol jerárquico.
   - Justificación: limpieza por tamaño acumulado requiere contexto de jerarquía.
2. **Representación de tamaño**
   - Tamaño acumulado mostrado en carpeta.
   - Tamaño individual mostrado en archivo.
3. **Expandir/colapsar**
   - `Enter` expande/colapsa si hay hijos; si no, entra.
   - `Backspace` colapsa/sale.
4. **Breadcrumbs**
   - Barra superior con ruta actual y nivel.

## 4) Métricas y ordenamientos
1. **Métricas visibles**
   - Tamaño total (acumulado).
   - Cantidad de archivos en carpeta.
   - Última modificación.
2. **Ordenamientos**
   - Tamaño (default).
   - Nombre.
   - Fecha de modificación.
3. **Filtros**
   - Ocultos (toggle).
   - Extensiones (filtro rápido).
   - Tamaño mínimo (opcional, no default).

## 5) Reglas de seguridad
1. **Confirmaciones obligatorias**
   - Siempre para borrar.
   - Doble confirmación para carpetas.
2. **Protección de rutas críticas**
   - Bloqueo explícito para `/`, `$HOME`, `/etc`, `/usr`, `/var` (configurable más adelante).
3. **Papelera**
   - MVP: no papelera, borrado directo con confirmación.
4. **Undo básico**
   - MVP: no undo.
   - Se registra un log de acciones para revisión.

## Resumen ejecutivo Fase 1
1. MVP centrado en navegación y gestión básica con seguridad estricta.
2. Vista inicial en árbol con tamaños acumulados como eje principal.
3. Acciones críticas con confirmaciones obligatorias y bloqueo de rutas críticas.
4. Sin backups avanzados, compresión o sincronización automática en MVP.
5. UX define acciones; ejecución desacoplada de UI para seguridad y test.
