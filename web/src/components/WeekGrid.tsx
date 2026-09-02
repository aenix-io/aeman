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
import type { ReactNode } from "react";
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
  onPointerDown?: React.PointerEventHandler<HTMLDivElement>;
  onPointerLeave?: React.PointerEventHandler<HTMLDivElement>;
  onPointerCancel?: React.PointerEventHandler<HTMLDivElement>;
}

/** What a week's label does when it is clicked, and what it says on hover. */
export interface WeekProps {
  title?: string;
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
  /** Slots, deadlines and drafts, placed into the grid by the board. */
  children?: ReactNode;
  /** Labels for the buttons at either end, which say what the rows are. */
  earlier?: string;
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
  children,
  earlier = "↑ earlier weeks",
  later = "↓ later weeks",
}: WeekGridProps<C>) {
  const { weeks, todayRow, rowH, sharedCol, colFactors } = grid;
  return (
    <div className="project-board" ref={grid.scrollRef}>
      <button
        type="button"
        className="project-more"
        onClick={grid.showEarlier}
        title={`Show ${WEEK_STEP} more weeks before`}
      >
        {earlier}
      </button>
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
          gridTemplateRows: `${HEADER_PX}px repeat(${weeks.length}, ${rowH}px)`,
        }}
      >
        {/* header row */}
        <div className="project-corner">{corner}</div>
        {columns.map((c, i) => head(c, i))}
        {gutter}

        {/* week label column + row stripes */}
        {weeks.map((w, i) => (
          <div
            key={w}
            className={`project-week${i === todayRow ? " project-week-today" : ""}`}
            style={{ gridRow: i + 2, gridColumn: 1 }}
            {...weekProps?.(w, i)}
          >
            <span className="project-week-date">{weekLabel(w)}</span>
          </div>
        ))}

        {/* cells: one per column × week, the drag surface */}
        {columns.map((c, col) =>
          weeks.map((w, row) => {
            const { className, ...rest } = cellProps?.(c, col, w, row) ?? {};
            return (
              <div
                key={`${c.key}/${w}`}
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
