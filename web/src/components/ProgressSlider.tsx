interface ProgressSliderProps {
  value: number;
  onChange: (value: number) => void;
}

/** ProgressSlider is a controlled 0..100 range input with a live label. */
export function ProgressSlider({ value, onChange }: ProgressSliderProps) {
  return (
    <label className="slider">
      <input
        type="range"
        min={0}
        max={100}
        step={5}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        aria-label="Readiness"
      />
      <span className="slider-value">{value}%</span>
    </label>
  );
}
