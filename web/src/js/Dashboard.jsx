import React, { Component } from "react";
import WebSocketHandler from "./WebSocketHandler";
import {getAuth} from "./auth"

export default class Dashboard extends Component {

  state = {
    reports: [],
    userLookupLoading: false,
    userLookupData: null,
  };

  constructor(props) {
    super(props);

    this.ws = new WebSocketHandler(props.wshost, getAuth());

    this.ws.subscribe('ReportReceived', (json) => {
      this.addToVisibleReports(json);
    });

    this.ws.subscribe('ReportHandled', (json) => {
      this.removeVisibleReport(json.ReportID);
    });

    this.ws.connect();
  }

  render() {
    return (
      <div className="row dashboard">
        <div className="col reports">
          <h4>Reports</h4>
          {this.state.reports.map((report, index) =>
            <div className="report card mb-3" key={index}>
              <div className="card-header">
                <i className={report.Channel.Type === "twitch" ? "fab fa-twitch" : "fab fa-discord"} />&nbsp;{report.Channel.Name}&nbsp;
                <span className="reporter">{report.Reporter.Name}</span>&nbsp;reported&nbsp;<span className="target">{report.Target.Name}</span>
              </div>
              <div className="card-body">
                {report.Reason ? <span className="reason">{report.Reason}</span> : null}
                <a target="_blank" href={`https://api.gempir.com/channel/${report.Channel.Name}/user/${report.Target.Name}`}>&nbsp;logs</a>
                <div>{report.Time}</div>
                {report.Logs && report.Logs.map((value, key) =>
                  <div key={key}>{value}</div>
                )}
                <button className="card-link btn btn-danger" title="Bans the user" onClick={() => this.handleReport(report.Channel.ID, report.ID, "ban")}>Ban</button>
                <button className="card-link btn btn-danger" title="Bans the user" onClick={() => this.handleReport(report.Channel.ID, report.ID, "timeout", 86400)}>Timeout 1d</button>
                <button className="card-link btn btn-danger" title="Bans the user" onClick={() => this.handleReport(report.Channel.ID, report.ID, "timeout", 604800)}>Timeout 7d</button>
                <button className="card-link btn btn-danger" title="Bans the user" onClick={() => this.handleReport(report.Channel.ID, report.ID, "timeout", 1209600)}>Timeout 14d</button>
                <button className="card-link btn btn-danger" title="Bans the user" onClick={() => this.handleReport(report.Channel.ID, report.ID, "dismiss")}>Dismiss</button>
                <button className="card-link btn btn-danger" title="Undos timeout/ban" onClick={() => this.handleReport(report.Channel.ID, report.ID, "undo")}>Undo</button>
                <button className="card-link btn btn-light" title="Hide this from your session" onClick={() => this.removeVisibleReport(report.ID)}>Hide</button>
              </div>
            </div>
          )}
        </div>

        <div className="col userLookup">
          <h4>User lookup</h4>
          <form className="inline-group" onSubmit={this.lookupUser}>
            <div className="input-group input-group-sm mb-3">
              <input type="text" className="form-control" placeholder="Recipient's username" aria-label="Recipient's username" aria-describedby="button-addon2" />
              <div className="input-group-append">
                <input type="submit" className="btn btn-outline-secondary" type="submit" id="button-addon2" value="Look up user" />
              </div>
            </div>
          </form>
          <span hidden={this.state.userLookupLoading === false}>Loading...</span>
          {this.state.userLookupData && (
            this.state.userLookupData.Actions.length > 0 ? (
              <div className="userData">
                <span>Listing latest <strong>{this.state.userLookupData.Actions.length}</strong> moderation actions on <strong>{this.state.userLookupName}</strong></span>
                <ul className="list-group">
                {this.state.userLookupData.Actions.map((action, index) =>
                <li className="list-group-item" key={index}>
                  <span>[{action.Timestamp}] {action.UserName} {action.Action}ed {this.state.userLookupName} for {action.Duration}s: {action.Reason}</span>
                </li>
                )}
                </ul>
              </div>
            ) : 'lol no bans'
                )}
        </div>
      </div>
    );
  }

  handleReport = (channelId, reportId, action, duration = null) => {
    let payload = {
      'Action': action,
      'ChannelID': channelId,
      'ReportID': reportId,
    };

    if (action === "dismiss") {
      this.removeVisibleReport(reportId);
    }

    if (duration) {
      payload['Duration'] = duration;
    }

    this.ws.publish('HandleReport', payload);
  }

  removeVisibleReport = (id) => {
    const newReports = [];

    this.state.reports.map(report => {
      if (report.ID !== id){
        newReports.push(report);
      }
    });

    this.setState({
      ...this.state,
      reports: newReports
    });
  }

  addToVisibleReports = (report) => {

    const newReports = this.state.reports;
    newReports.push(report);

    this.setState({
      ...this.state,
      reports: newReports,
    })
  }

  hasUserData = () => {
    return this.state.userLookupData !== null;
  }

  lookupUser = (e) => {
    e.preventDefault();

    let username = e.target[0].value;

    if (username.length < 2) {
      return;
    }

    this.setState({
      userLookupLoading: true,
      userLookupName: username,
    });

    fetch('/api/channel/22484632/moderation/user?user_name='+username)
      .then((response) => {
        return response.json();
      })
      .then((myJson) => {
        console.log('Results:', myJson);
        this.setState({
          userLookupLoading: false,
          userLookupData: myJson,
        });
      });
  }
}
