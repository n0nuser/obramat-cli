import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

/** @type {import('next').NextConfig} */
export default withMDX({
  reactStrictMode: true,
  // Static export: the site is plain files, hosted on GitHub Pages.
  output: 'export',
  trailingSlash: true,
});
