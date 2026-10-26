fun describe(x: Int): String = when (x) {
    1 -> "one"                // code_branch 1
    2, 3 -> "few"             // code_branch 1 (one entry)
    else -> "many"            // 0
}                             // total 2

fun chain(a: Boolean, b: Boolean, c: Boolean): Int =
    if (a) 1 else if (b) 2 else if (c) 3 else 4        // code_branch 4 (3 ifs + final else)

fun safe(s: String?): Int = s?.trim()?.length ?: 0
// code_branch 0 (?. is not a branch), condition 2 (clauses of ?:), total 2

fun loops(xs: List<Int>, m: Map<String, Int>) {
    for (x in xs) { }         // code_branch 1, local_variable 1
    for ((k, v) in m) { }     // code_branch 1, local_variable 1
    while (a) { }             // code_branch 1
    do { } while (b)          // code_branch 1
    x!!.y                     // 0
}                             // code_branch 4, local_variable 2

fun subjectless(a: Int, b: Boolean) = when {
    a > 1 -> x()              // code_branch 1, condition 0
    b -> y()                  // code_branch 1
    else -> z()               // 0
}                             // code_branch 2

class Holder {
    fun body(x: Int) = when (x) {
        0 -> "zero"           // code_branch 1
        else -> "other"
    }
    val f = { x: Int ->       // lambda 1, local_variable 1
        when (x) {
            1 -> "one"        // code_branch 1
            else -> "other"
        }
    }
}                             // code_branch 2

fun deep(a: A?) = a?.b?.c?.d  // code_branch 0
fun plain(a: A) = a.b.c.d     // code_branch 0
fun let(x: String?) = x?.let { it.length }   // code_branch 0, lambda 1
fun bang(x: String?) {
    x!!.y                     // 0
    x!!                       // 0
}
fun guardedIf(a: Boolean, b: Boolean) = if (a && b) x else y   // code_branch 2, condition 2
fun elvisReturn(x: String?): String {
    val v = x ?: return ""    // condition 2, local_variable 1
    return v
}
fun elvisThrow(x: String?): String = x ?: throw E()   // condition 2
fun tryValue(): Int {
    val v = try { a() } catch (e: E) { 0 }   // exception_handling 2, local_variable 1
    return v
}
