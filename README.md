makeWebBook
===========

Go program to build a scientific web book. All the details are described
in the web book [Building Scientific Web Books with makeWebBook](http://martinotter.github.io/BuildingScientificWebBooks/Tutorial/)

A book consists of a set of HTML files. A configuration file
(configuration.json) defines the order of the files. With the command

makeWebBook bookDirectory

the following actions are preformed provided a corresponding element
starts with the text "Chapter" or "Appendix" (otherwise the section is
not modified; this is useful for a preface or a reference chapter):

-   \<h1\>, \<h2\>, \<h3\>, \<h4\> elements are updated with section
    numbers.

-   \<caption\> elements are updated with table numbers.

-   \<figcaption\> elements are updated with figure numbers.

-   \<div class="equation"\> \$\$ ... \$\$ \</div\> are updated with
    equation numbers.

-   \<a href=”..”\>..\</a\> elements are updated with file name, link
    text and tool tip if the link points to a position in the book.

If a number is not present, it is introduced (with exception of \<h1\>
element, where a number is only introduced if the text starts with
"Chapter" or with "Appendix"). If it is present and correct, nothing is
changed. Otherwise, the number is updated.

A navigation bar is introduced in all files with links to the "table of
contents" file, the previous, and the next file.

The "table of contents" file is updated with the actual document
structure.

The makeWebBook.go program is licensed under the 
[Creative Commons Attribution-NonCommercial-ShareAlike 4.0 International License](http://creativecommons.org/licenses/by-nc-sa/4.0/).
