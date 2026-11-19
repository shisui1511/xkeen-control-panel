import { syntaxTree } from '@codemirror/language';
import type { EditorState } from '@codemirror/state';
import type { SyntaxNode } from '@lezer/common';

export interface PathSegment {
  label: string;
  pos: number;
}

/**
 * Строит интерактивный путь (хлебные крошки) к объекту под курсором на основе AST дерева.
 */
export function buildPathAtCursor(state: EditorState, pos: number, isYaml: boolean): PathSegment[] {
  const segments: PathSegment[] = [];
  const tree = syntaxTree(state);
  if (!tree) return segments;

  const startNode: SyntaxNode | null = tree.resolveInner(pos, -1);
  let node = startNode;

  while (node && node.parent) {
    if (!isYaml) {
      // --- Алгоритм разбора JSON ---
      if (node.name === 'Property') {
        const nameNode = node.getChild('PropertyName');
        if (nameNode) {
          let key = state.doc.sliceString(nameNode.from, nameNode.to);
          // Очистка от кавычек
          if (
            (key.startsWith('"') && key.endsWith('"')) ||
            (key.startsWith("'") && key.endsWith("'"))
          ) {
            key = key.slice(1, -1);
          }
          segments.unshift({ label: key, pos: nameNode.from });
        }
      } else if (node.name === 'Array') {
        // Определяем, из какого ребенка массива мы поднялись
        let childNode: SyntaxNode | null = startNode;
        while (childNode && childNode.parent !== node) {
          childNode = childNode.parent;
        }
        if (childNode) {
          // Подсчитываем предшествующие элементы (игнорируем скобки и запятые)
          let index = 0;
          let sibling = node.firstChild;
          while (sibling && sibling.from < childNode.from) {
            if (sibling.name !== '[' && sibling.name !== ']' && sibling.name !== ',') {
              index++;
            }
            sibling = sibling.nextSibling;
          }
          segments.unshift({ label: `[${index}]`, pos: childNode.from });
        }
      }
    } else {
      // --- Алгоритм разбора YAML ---
      if (node.name === 'Pair') {
        const keyNode = node.getChild('Key');
        if (keyNode) {
          let key = state.doc.sliceString(keyNode.from, keyNode.to).trim();
          if (key.endsWith(':')) {
            key = key.slice(0, -1).trim();
          }
          if (
            (key.startsWith('"') && key.endsWith('"')) ||
            (key.startsWith("'") && key.endsWith("'"))
          ) {
            key = key.slice(1, -1);
          }
          segments.unshift({ label: key, pos: keyNode.from });
        }
      } else if (node.name === 'BlockSequence') {
        let childNode: SyntaxNode | null = startNode;
        while (childNode && childNode.parent !== node) {
          childNode = childNode.parent;
        }
        if (childNode) {
          // Считаем только узлы SequenceItem
          let index = 0;
          let sibling = node.firstChild;
          while (sibling && sibling.from < childNode.from) {
            if (sibling.name === 'SequenceItem') {
              index++;
            }
            sibling = sibling.nextSibling;
          }
          segments.unshift({ label: `[${index}]`, pos: childNode.from });
        }
      }
    }
    node = node.parent;
  }

  return segments;
}

/**
 * Заменяет комментарии JSONC пробелами, сохраняя переводы строк: длина результата
 * равна длине исходного текста, поэтому позиции ошибок JSON-парсера указывают
 * на исходный текст.
 */
export function blankJsonComments(text: string): string {
  let out = '';
  let inString = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (inString) {
      out += c;
      if (c === '\\' && i + 1 < text.length) out += text[++i];
      else if (c === '"') inString = false;
      continue;
    }
    if (c === '"') {
      inString = true;
      out += c;
    } else if (c === '/' && text[i + 1] === '/') {
      while (i < text.length && text[i] !== '\n') {
        out += text[i] === '\r' ? '\r' : ' ';
        i++;
      }
      if (i < text.length) out += '\n';
    } else if (c === '/' && text[i + 1] === '*') {
      const end = text.indexOf('*/', i + 2);
      const stop = end === -1 ? text.length : end + 2;
      for (; i < stop; i++) out += text[i] === '\n' || text[i] === '\r' ? text[i] : ' ';
      i--;
    } else {
      out += c;
    }
  }
  return out;
}
