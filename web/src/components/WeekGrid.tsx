/** The week grid: a table whose rows are weeks and whose columns are whatever
 *  the board puts along the top.
 *
 * This is the scaffolding only — the scroller, the two "more weeks" buttons,
 * the sticky corner, the header row, the week labels down the left and the
 * empty cells that are the drag surface. What stands IN the grid — the slots,
 * the deadlines, a draft being pulled out — is the board's, and is passed as
 * children: they place themselves with `gridRow` / `gridColumn` into the same
 * grid, which is what lets a slot span weeks and still sit above the cells.
 *
 * The header cells are the board's too. A column here is a key and nothing
 * more; whether it names an epic or a person is a question this file must
 * never be able to ask.
 */
import type { ReactNode, Ref } from "react";
import { GUTTER_PX, HEADER_PX, WEEK_COL_PX, WEEK_STEP, weekLabel } from "../weekgrid";
import type { WeekGrid as Grid } from "./useWeekGrid";

/** What the grid needs of a column: a key of its own, and a width when it has
 *  been given one. */
export interface GridColumn {
  key: string;
}

/** The attributes a board adds to one cell of the surface — the drag
 *  highlight and the pointer handlers that start a gesture. */
export interface CellProps {
  className?: string;
  /** A board that drops onto its cells attaches its drop target here. */
  ref?: Ref<HTMLDivElement>;
  onPointerDown?: React.PointerEventHandler<HTMLDivElement>;
  onPointerLeave?: React.PointerEventHandler<HTMLDivElement>;
  onPointerCancel?: React.PointerEventHandler<HTMLDivElement>;
}

/** What a week's cell says, and what it does when it is clicked. `label`
 *  replaces the date the cell shows by default — a board that carries a count
 *  or a limit beside the date puts the whole thing here. */
export interface WeekProps {
  title?: string;
  className?: string;
  label?: ReactNode;
  onClick?: React.MouseEventHandler<HTMLDivElement>;
}

export interface WeekGridProps<C extends GridColumn> {
  grid: Grid;
  columns: readonly C[];
  /** The top-left cell, above the week labels. */
  corner?: ReactNode;
  /** One header per column, in order. */
  head: (column: C, index: number) => ReactNode;
  /** The head of the trailing gutter — where a board that can gain columns
   *  offers to add one. */
  gutter?: ReactNode;
  weekProps?: (week: string, row: number) => WeekProps;
  cellProps?: (column: C, col: number, week: string, row: number) => CellProps;
  /** How tall each row is, in pixels, when they are not all the same — the
   *  board works that out from what stands in them (weekgrid.rowHeights).
   *  Left out, every row is the one height the zoom sets. */
  rowHeights?: readonly number[];
  /** Slots, deadlines and drafts, placed into the grid by the board. */
  children?: ReactNode;
  /** Labels for the buttons at either end. A board whose first row is this
   *  week — because what is overdue is shown as owed now — passes false for
   *  `earlier`: there is no past to widen into. */
  earlier?: string | false;
  later?: string;
}

export function WeekGrid<C extends GridColumn>({
  grid,
  columns,
  corner,
  head,
  gutter,
  weekProps,
  cellProps,
  rowHeights,
  children,
  earlier = "↑ earlier weeks",
  later = "↓ later weeks",
}: WeekGridProps<C>) {
  const { weeks, todayRow, rowH, sharedCol, colFactors } = grid;
  return (
    <div className="project-board" ref={grid.scrollRef}>
      {earlier !== false && (
        <button
          type="button"
          className="project-more"
          onClick={grid.showEarlier}
          title={`Show ${WEEK_STEP} more weeks before`}
        >
          {earlier}
        </button>
      )}
      <div
        className="project-grid"
        ref={grid.gridRef}
        style={{
          // Until the columns are dragged they share the room; once dragged
          // they all take the width that was chosen.
          // The week column is what "17 Aug" needs and no more.
          // Every column is sized explicitly: a column with a width of its
          // own takes its ratio of the shared width, the rest take the shared
          // width itself. Text is never scaled — only the room around it.
          gridTemplateColumns: `${WEEK_COL_PX}px ${columns
            .map((c) => `${Math.round(sharedCol * (colFactors[c.key] ?? 1))}px`)
            .join(" ")} ${GUTTER_PX}px`,
          // Every row is spelled out in pixels when they differ, because a
          // slot's band is a percentage of the rows it covers and a
          // percentage needs something definite to be a percentage of.
          gridTemplateRows: rowHeights
            ? `${HEADER_PX}px ${rowHeights.map((h) => `${h}px`).join(" ")}`
            : `${HEADER_PX}px repeat(${weeks.length}, ${rowH}px)`,
        }}
      >
        {/* header row */}
        <div className="project-corner">{corner}</div>
        {columns.map((c, i) => head(c, i))}
        {gutter}

        {/* week label column + row stripes */}
        {weeks.map((w, i) => {
          const { className, label, ...rest } = weekProps?.(w, i) ?? {};
          return (
            <div
              key={w}
              className={`project-week${i === todayRow ? " project-week-today" : ""}${
                className ? ` ${className}` : ""
              }`}
              style={{ gridRow: i + 2, gridColumn: 1 }}
              {...rest}
            >
              {label ?? <span className="project-week-date">{weekLabel(w)}</span>}
            </div>
          );
        })}

        {/* cells: one per column × week, the drag surface */}
        {columns.map((c, col) =>
          weeks.map((w, row) => {
            const { className, ref, ...rest } = cellProps?.(c, col, w, row) ?? {};
            return (
              <div
                key={`${c.key}/${w}`}
                ref={ref}
                // Every cell says which week and which column it is: a
                // gesture can then ask the DOM under the pointer instead of
                // measuring tracks itself.
                data-week={w}
                data-col={c.key}
                className={`project-cell${row === todayRow ? " project-cell-today" : ""}${
                  className ? ` ${className}` : ""
                }`}
                style={{ gridRow: row + 2, gridColumn: col + 2 }}
                {...rest}
              />
            );
          }),
        )}

        {children}
      </div>
      <button
        type="button"
        className="project-more"
        onClick={grid.showLater}
        title={`Show ${WEEK_STEP} more weeks after`}
      >
        {later}
      </button>
    </div>
  );
}
