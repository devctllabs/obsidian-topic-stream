import { requestUrl } from "obsidian";

import type { HttpTransport } from "../../api/transport";

export const obsidianHttp: HttpTransport = (request) => requestUrl({ ...request, throw: false });
