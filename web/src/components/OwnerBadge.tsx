// OwnerBadge is whose a task is, drawn the same wherever an owner is shown —
// the Roadmap's Owner column and the Backlog card: the TEAM's avatar as the
// Me and Team boards draw it (the coloured circle with its initial); with a
// person, the person's avatar to its right, OVERLAPPING it by half its width
// and on top of it; and beside the group one label — the team when nobody
// has the task, the person when somebody does, "No team" for the no-team
// group.
import { teamColor, teamInitial } from "../avatar";
import type { Owner } from "../owner";
import { displayName, type Avatars, type Names } from "../users";
import { Avatar } from "./Avatar";

export function OwnerBadge({ owner, avatars, names }: { owner: Owner; avatars?: Avatars; names?: Names }) {
  const team = owner.team;
  const label = owner.person ? displayName(owner.person, names) : team || "No team";
  return (
    <span className="owner-badge">
      <span className={`owner-badge-stack${owner.person ? " owner-badge-overlap" : ""}`}>
        {team ? (
          <span
            className="team-avatar owner-badge-team"
            style={{ backgroundColor: teamColor(team) }}
            title={`Team ${team}`}
          >
            {teamInitial(team)}
          </span>
        ) : (
          <span className="team-avatar team-avatar-empty owner-badge-team" title="No team" />
        )}
        {owner.person && (
          <Avatar
            login={owner.person}
            avatars={avatars}
            names={names}
            className="avatar-img owner-badge-person"
            title={displayName(owner.person, names)}
          />
        )}
      </span>
      <span className="owner-badge-label">{label}</span>
    </span>
  );
}
