## Description: <br>
Fetches Douyin hot list and trending-search data, including ranks, topic titles, heat values, and links. <br>

This skill is ready for commercial/non-commercial use. <br>

## Publisher: <br>
[noah-1106](https://clawhub.ai/user/noah-1106) <br>

### License/Terms of Use: <br>
MIT-0 <br>


## Use Case: <br>
Developers, social media operators, analysts, and marketing teams use this skill to retrieve Douyin trending topics for hot topic tracking, trend analysis, content planning, and social operations. <br>

### Deployment Geography for Use: <br>
Global <br>

## Known Risks and Mitigations: <br>
Risk: Bundled helper scripts can retain fetched trend history in a local SQLite database and generate an HTML report. <br>
Mitigation: Review the helper scripts before installation, avoid sharing generated reports until output escaping and link validation are confirmed, and delete data/douyin.db when retained history is no longer needed. <br>
Risk: The public Douyin web interface may change or rate-limit frequent requests. <br>
Mitigation: Use reasonable request frequency and verify fetched results before relying on them for trend analysis or planning. <br>


## Reference(s): <br>
- [ClawHub skill page](https://clawhub.ai/noah-1106/douyin-hot) <br>
- [Publisher profile](https://clawhub.ai/user/noah-1106) <br>
- [Douyin website](https://www.douyin.com/) <br>


## Skill Output: <br>
**Output Type(s):** [text, JSON, shell commands, files] <br>
**Output Format:** [Terminal text, JSON arrays, and optional local HTML reports containing rank, title, heat value, label, and link fields.] <br>
**Output Parameters:** [1D] <br>
**Other Properties Related to Output:** [Default fetch limit is 50 items; helper scripts can store trend history in a local SQLite database and generate a local HTML report.] <br>

## Skill Version(s): <br>
1.0.8 (source: ClawHub release metadata) <br>

## Ethical Considerations: <br>
Users should evaluate whether this skill is appropriate for their environment, review any generated or modified files before relying on them, and apply their organization's safety, security, and compliance requirements before deployment. <br>
