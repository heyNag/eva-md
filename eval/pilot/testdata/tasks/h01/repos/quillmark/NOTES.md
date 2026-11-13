# quillmark

quillmark reads a text and renders a short report, one line a figure, in
this order:

    wordtally: <words, split on white space>
    linetally: <lines; a last line without a newline counts>
    widestword: <the longest word; the first one when two are as long>
    uniqtally: <distinct words, upper and lower case taken as one>

An empty text renders each count as 0 and widestword as a dash.
