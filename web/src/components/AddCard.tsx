import { useState } from "react";

interface AddCardProps {
  onCreate: (title: string) => void;
  placeholder?: string;
}

/** AddCard is a subtle inline control that expands into a title input. */
export function AddCard({ onCreate, placeholder = "Add a card…" }: AddCardProps) {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");

  const close = () => {
    setOpen(false);
    setValue("");
  };

  const submit = () => {
    const title = value.trim();
    if (title) {
      onCreate(title);
    }
    close();
  };

  if (!open) {
    return (
      <button
        type="button"
        className="add-card"
        onClick={() => setOpen(true)}
      >
        + add
      </button>
    );
  }

  return (
    <input
      type="text"
      className="add-card-input"
      autoFocus
      value={value}
      placeholder={placeholder}
      onChange={(e) => setValue(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          submit();
        } else if (e.key === "Escape") {
          close();
        }
      }}
      onBlur={close}
    />
  );
}
