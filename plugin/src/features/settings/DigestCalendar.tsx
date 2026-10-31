import type { Schema } from "../../api/contracts";

type Recurrence = Schema<"Recurrence">;
export function DigestCalendar({ value, onChange }: { value: Recurrence; onChange: (value: Recurrence) => void }) {
  const changeFrequency = (frequency: Recurrence["frequency"]) => {
    const clock = { hour: value.hour, minute: value.minute };
    if (frequency === "weekly") onChange({ ...clock, frequency, weekdays: [1] });
    else if (frequency === "monthly") onChange({ ...clock, frequency, rule: { type: "dayOfMonth", day: 1 } });
    else onChange({ ...clock, frequency });
  };
  return <div className="topic-stream-advanced-fields">
    <label>Repeat<select value={value.frequency} onChange={(event) => changeFrequency(event.target.value as Recurrence["frequency"])}><option value="daily">Daily</option><option value="weekly">Weekly</option><option value="monthly">Monthly</option></select></label>
    <label>Time<input type="time" required value={`${String(value.hour).padStart(2, "0")}:${String(value.minute).padStart(2, "0")}`} onChange={(event) => {
      if (!event.target.value) return;
      const [hour = 0, minute = 0] = event.target.value.split(":").map(Number);
      onChange({ ...value, hour, minute });
    }} /></label>
    {value.frequency === "weekly" && <fieldset><legend>Days</legend><div className="topic-stream-kind-options">{weekdays.map((day, index) => <label key={day} className="topic-stream-checkbox"><input type="checkbox" checked={value.weekdays.includes(index + 1)} onChange={(event) => onChange({ ...value, weekdays: event.target.checked ? [...value.weekdays, index + 1].sort() : value.weekdays.filter((number) => number !== index + 1) })} />{day}</label>)}</div></fieldset>}
    {value.frequency === "monthly" && <MonthlyRule value={value.rule} onChange={(rule) => onChange({ ...value, rule })} />}
  </div>;
}
const weekdays = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];
function MonthlyRule({ value, onChange }: { value: Schema<"MonthlyRecurrence">["rule"]; onChange: (value: Schema<"MonthlyRecurrence">["rule"]) => void }) {
  return <>
    <label>On<select value={value.type} onChange={(event) => {
      if (event.target.value === "lastDay") onChange({ type: "lastDay" });
      else if (event.target.value === "nthWeekday") onChange({ type: "nthWeekday", weekday: 1, occurrence: 1 });
      else onChange({ type: "dayOfMonth", day: 1 });
    }}><option value="dayOfMonth">Day of month</option><option value="nthWeekday">Weekday of month</option><option value="lastDay">Last day</option></select></label>
    {value.type === "dayOfMonth" && <label>Day<input type="number" min={1} max={31} value={value.day} onChange={(event) => onChange({ ...value, day: Number(event.target.value) })} /></label>}
    {value.type === "nthWeekday" && <>
      <label>Occurrence<select value={value.occurrence} onChange={(event) => onChange({ ...value, occurrence: Number(event.target.value) })}>{[1, 2, 3, 4, 5].map((number) => <option key={number} value={number}>{number}</option>)}</select></label>
      <label>Weekday<select value={value.weekday} onChange={(event) => onChange({ ...value, weekday: Number(event.target.value) })}>{weekdays.map((day, index) => <option key={day} value={index + 1}>{day}</option>)}</select></label>
    </>}
  </>;
}
