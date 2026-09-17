declare module 'markdown-it' {
  interface MarkdownItOptions {
    html?: boolean;
    linkify?: boolean;
    typographer?: boolean;
    breaks?: boolean;
  }
  export default class MarkdownIt {
    constructor(options?: MarkdownItOptions);
    render(src: string): string;
    renderInline(src: string): string;
  }
}
