import { useId, type InputHTMLAttributes } from "react";
import type { FieldError, UseFormRegisterReturn } from "react-hook-form";

export function SettingsTextField({ label, binding, error, ...props }: InputHTMLAttributes<HTMLInputElement> & {
  label: string; binding: UseFormRegisterReturn; error: FieldError | undefined;
}) {
  const id = useId();
  return <div><label htmlFor={id}>{label}</label><input {...props} {...binding} id={id} aria-invalid={!!error} aria-describedby={error ? `${id}-error` : undefined} />
    {error && <p id={`${id}-error`} role="alert" className="topic-stream-inline-error">{error.message}</p>}
  </div>;
}
