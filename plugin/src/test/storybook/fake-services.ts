import { deferred, type Deferred } from "./deferred";

export class ControlledAction<Input, Output> {
  public readonly calls: Array<{ input: Input; response: Deferred<Output> }> = [];

  public invoke = (input: Input): Promise<Output> => {
    const response = deferred<Output>();
    this.calls.push({ input, response });
    return response.promise;
  };
}

export type { ViewEvent as FixtureAdvisory } from "../../shared/events";
import type { ViewEvent as FixtureAdvisory } from "../../shared/events";

export class FixtureEvents {
  private readonly listeners = new Set<(event: FixtureAdvisory) => void>();

  public subscribe(listener: (event: FixtureAdvisory) => void): () => void {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  }

  public emit(event: FixtureAdvisory): void {
    for (const listener of this.listeners) listener(event);
  }
}

export { deferred };
