# Sources and ingestion

Sources determine where Topic Stream discovers content. Users can create, inspect, update, enable,
disable, and delete RSS/Atom, arXiv query, GitHub Trending, and YouTube channel Sources. Each stores
its own polling interval, from 15 minutes to seven days.

| Source | Configuration | Item kind |
| --- | --- | --- |
| RSS/Atom | Feed URL and private-network policy | Article |
| arXiv | Query | Paper |
| GitHub Trending | Period and language | Repository |
| YouTube | Channel ID | Video |

Sync now requests work for one Source; Sync all now requests work for every enabled Source.
Failures are independent, so one Source does not block the others. Scheduled polling discovers
Sources whose intervals have elapsed. Synchronization is durable and follows the
[Fetch Queue retry policy](durable-jobs.md).

Items are canonical: repeated synchronization or discovery through multiple Sources does not create
multiple copies of the same content. Each Source Entry preserves its provenance. Articles provide
cleaned full content when extraction succeeds; arXiv provides paper metadata and abstracts; YouTube
provides video descriptions without transcripts; repositories provide metadata and README content.

Synchronization and extraction failures remain visible through the Job's diagnostic reason.
Implementation and extraction-version details belong in
[content ingestion](../../architecture/content-ingestion-and-reader.md).
