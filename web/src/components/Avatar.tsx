import type { CSSProperties } from "react";
import { initials } from "../avatar";
import { avatarUrlFor, type Avatars } from "../users";

interface AvatarProps {
  login: string;
  /** The roster's login → URL map (GET /board members). */
  avatars?: Avatars;
  /** Sizing class(es); defaults to the boards' 20px `avatar-img`. */
  className?: string;
  title?: string;
  style?: CSSProperties;
  draggable?: boolean;
}

/** Avatar draws a person: the roster's picture when they are a member, their
 *  initials in the same circle when they are not (an assignee typed by login
 *  has no picture the server knows of). */
export function Avatar({
  login,
  avatars,
  className = "avatar-img",
  title,
  style,
  draggable,
}: AvatarProps) {
  const url = avatarUrlFor(login, avatars);
  if (url) {
    return (
      <img
        className={className}
        src={url}
        alt={login}
        title={title}
        style={style}
        draggable={draggable}
      />
    );
  }
  return (
    <span
      className={`${className} avatar-initials`}
      role="img"
      aria-label={login}
      title={title}
      style={style}
    >
      {initials(login)}
    </span>
  );
}
