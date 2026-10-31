export function FeedMessage({ title, detail }: { title: string; detail: string }) {
  return (
    <section className="topic-stream-message" role="status">
      <h2>{title}</h2>
      <p>{detail}</p>
    </section>
  );
}
