import { ApiError } from "../../api/errors";

interface ApiErrorMessageProps {
  error: unknown;
  title: string;
}

/** ApiErrorMessage displays one operation's failure without changing the owning form's state. */
export function ApiErrorMessage({ error, title }: ApiErrorMessageProps) {
  const problem = error instanceof ApiError ? error : null;
  const message = error instanceof Error ? error.message : "The operation could not be completed.";
  return (
    <div className="topic-stream-inline-error" role="alert">
      <strong>{problem?.canonicalCommitted ? "Change saved" : title}</strong>
      <p>{message}</p>
      {problem !== null && problem.issues.length > 0 && (
        <ul>
          {problem.issues.map((issue, index) => (
            <li key={`${issue.field}:${index}`}>
              {issue.field !== "" && <span>{issue.field}: </span>}{issue.message}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
