import { llms } from 'fumadocs-core/source';
import { source } from '@/lib/source';

export const dynamic = 'force-static';

export async function GET() {
  return new Response(await llms(source).index(), { headers: { 'content-type': 'text/plain; charset=utf-8' } });
}
