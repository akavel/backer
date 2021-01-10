@echo off
if not "%1"=="" goto :%1
goto:default

:default
:exp-view
go run -v -tags purego ./exp-view "%2" "%3" "%4" "%5"
goto:eof

:eof
