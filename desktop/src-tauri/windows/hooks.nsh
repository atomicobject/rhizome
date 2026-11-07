!macro NSIS_HOOK_POSTINSTALL
  StrCpy $R0 "-1"
  ExecWait '"$INSTDIR\${MAINBINARYNAME}.exe" --install-desktop-integration' $R0
  ${If} $R0 != 0
    MessageBox MB_ICONSTOP|MB_OK "Rhizome was installed, but its desktop integration could not be registered. Open Rhizome and choose Settings > Install Command-Line Tool to select this installation."
    Abort
  ${EndIf}
!macroend

!macro NSIS_HOOK_PREUNINSTALL
  ${If} $UpdateMode <> 1
    !insertmacro CheckIfAppIsRunning "$INSTDIR\${MAINBINARYNAME}.exe" "${PRODUCTNAME}"
    StrCpy $R0 "-1"
    ExecWait '"$INSTDIR\${MAINBINARYNAME}.exe" --remove-desktop-integration' $R0
    ${If} $R0 != 0
      MessageBox MB_ICONSTOP|MB_OK "Rhizome could not remove its desktop integration. Close Rhizome and any terminal commands using its rzm, then try uninstalling again. No repository or user data was removed."
      Abort
    ${EndIf}
  ${EndIf}
!macroend
