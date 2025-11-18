// Extracted from index.html for testing purposes
function formatR2RCallout(avgOpenHours, r2rSavings, currentEfficiency, modeledEfficiency, targetMergeHours = 1.5) {
    // Only show if average merge velocity is > target
    if (avgOpenHours <= targetMergeHours) {
        return '';
    }

    // Format savings with appropriate precision
    let savingsText;
    if (r2rSavings >= 1000000) {
        savingsText = '$' + (r2rSavings / 1000000).toFixed(1) + 'M';
    } else if (r2rSavings >= 1000) {
        savingsText = '$' + (r2rSavings / 1000).toFixed(0) + 'K';
    } else {
        savingsText = '$' + r2rSavings.toFixed(0);
    }

    const efficiencyDelta = modeledEfficiency - currentEfficiency;

    // Format target merge time
    let targetText = targetMergeHours.toFixed(1) + 'h';

    let html = '<div style="margin: 24px 0; padding: 12px 20px; background: linear-gradient(135deg, #e6f9f0 0%, #ffffff 100%); border: 1px solid #00c853; border-radius: 8px; font-size: 16px; color: #1d1d1f; line-height: 1.6; font-family: -apple-system, BlinkMacSystemFont, \'Segoe UI\', Helvetica, Arial, sans-serif, \'Apple Color Emoji\', \'Segoe UI Emoji\', \'Noto Color Emoji\';">';
    html += '<span style="font-family: \'Apple Color Emoji\', \'Segoe UI Emoji\', \'Noto Color Emoji\', sans-serif; font-style: normal; font-weight: normal; text-rendering: optimizeLegibility;">\uD83D\uDCA1</span> <strong>Pro-Tip:</strong> Boost team throughput by <strong>' + efficiencyDelta.toFixed(1) + '%</strong> and save <strong>' + savingsText + '/yr</strong> by reducing merge times to &lt;' + targetText + ' with ';
    html += '<a href="https://codegroove.dev/products/ready-to-review/" target="_blank" rel="noopener" style="color: #00c853; font-weight: 600; text-decoration: none;">Ready to Review</a>. ';
    html += 'Free for open-source repositories, $6/user/org for private repos.';
    html += '</div>';
    return html;
}

// Export for testing (Node.js) or use globally (browser)
if (typeof module !== 'undefined' && module.exports) {
    module.exports = { formatR2RCallout };
}
