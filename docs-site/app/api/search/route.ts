import { createFromSource } from 'fumadocs-core/search/server';
import { source } from '@/lib/source';

// Built once at export time; the browser searches the static index.
export const dynamic = 'force-static';
export const { staticGET: GET } = createFromSource(source, { language: 'english' });
