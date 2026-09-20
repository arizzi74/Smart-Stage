# Read-only context for a failed native display observation on an ephemeral runner.
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$root = [System.Windows.Automation.AutomationElement]::RootElement
$visible = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::IsOffscreenProperty, $false)
$elements = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, $visible)
$rows = @()
for ($index = 0; $index -lt [Math]::Min($elements.Count, 250); $index++) {
    $element = $elements.Item($index)
    try {
        $current = $element.Current
        $toggle = $null
        $toggleValue = $null
        if ($element.TryGetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern, [ref]$toggle)) {
            $toggleValue = $toggle.Current.ToggleState.ToString()
        }
        $invoke = $null
        $canInvoke = $element.TryGetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern, [ref]$invoke)
        $rectangle = $current.BoundingRectangle
        $rows += [ordered]@{
            name = $current.Name
            automationId = $current.AutomationId
            controlType = $current.ControlType.ProgrammaticName
            processId = $current.ProcessId
            toggle = $toggleValue
            canInvoke = $canInvoke
            bounds = @($rectangle.X, $rectangle.Y, $rectangle.Width, $rectangle.Height)
        }
    } catch { }
}
[ordered]@{ visibleElementCount = $elements.Count; truncated = $elements.Count -gt 250; elements = $rows } | ConvertTo-Json -Depth 6
