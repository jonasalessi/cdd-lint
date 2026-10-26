// code_branch fixture. Expected total for `branches`: 11.
export function branches(v: number, o: { a?: { b: number } }): number {
  if (v === 0) {
    // +1  if
    return 0;
  } else if (v === 1) {
    // +1  the chained if; its `else` adds nothing, so if/else-if/else is 3
    return 1;
  } else {
    // +1  else
    return 2;
  }

  switch (v) {
    // the switch itself adds nothing
    case 1:
      // +1  case
      break;
    case 2:
      // +1  case
      break;
    default:
      // +0  default
      break;
  }

  const t = v > 0 ? 1 : -1; // +1  ternary

  for (let i = 0; i < 1; i++) {
    // +1  for
    void i;
  }
  for (const k in o) {
    // +1  for…in
    void k;
  }
  for (const n of [1]) {
    // +1  for…of, which is a for_in_statement in the grammar
    void n;
  }
  while (v > 5) {
    // +1  while
    break;
  }
  do {
    // +1  do…while
    break;
  } while (v > 5);

  const shallow = o?.a; // +0  an optional chain is not a branch
  const deep = o?.a?.b; // +0

  return t + Number(shallow) + Number(deep);
}

// Expected total for `optionalCalls`: 0.
//
// `?.` is not a branch in any of its shapes: a member access (`a?.b`), a
// subscript access (`a?.[0]`) or an optional call (`a?.()`).
export function optionalCalls(f: any, o: any): unknown {
  const a = f?.(); // +0
  const b = o.f?.(); // +0
  const c = o?.f?.(); // +0
  const d = o?.[0]?.(); // +0
  const e = o?.b?.(1)?.[2]; // +0
  return [a, b, c, d, e];
}
