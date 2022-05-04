import Ember from 'ember';
import config from '../../config/environment';

export default Ember.Route.extend({
  auth: Ember.inject.service('auth'),
  focusUsername:"",
  actions: {
    error: function(reason, transition) {
      this.transitionTo('/login');
      console.log(reason + transition);
      return false;
    },
    focusline(name) {
      this.focusUsername = name
    },
    add(newValue) {
      console.log(newValue);
      var r = this;
      const { textID, selectedRule, selectedAlarm, textDesc } = this.get('controller').getProperties('textID', 'selectedRule', 'selectedAlarm', 'textDesc');
      if (!Ember.isEmpty(textID)) {
        var result = textID.match(/\b0x[0-9A-Fa-f]{40}|[0-9A-Fa-f]{40}\b/g);
        if (textDesc.match(/[0-9A-Za-z]{1,40}/g) === false) {
          alert("entered character and numberic");
          return false;
        }
        console.log(result,textID);
        if (result) {
          var url = config.APP.ApiUrl + 'api/saveidbound';
          return Ember.$.ajax({
            method: 'post',
            url: url,
            crossDomain: true,
            xhrFields: {
              withCredentials: true
            },
            data: JSON.stringify({
              ip: textID,
              rule: selectedRule,
              alarm: selectedAlarm,
              desc: textDesc
            })
          }).then(function(data) {
            Ember.run.later(r, r.refresh, 10);
            return data;
          });
        }
      }
    },
    delete(newValue) {
      console.log(newValue);
      var r = this;
      var url = config.APP.ApiUrl + 'api/delidbound';
      return Ember.$.ajax({
        method: 'post',
        url: url,
        crossDomain: true,
        xhrFields: {
          withCredentials: true
        },
        data: JSON.stringify({
          ip: newValue,
        })
      }).then(function(data) {
        Ember.run.later(r, r.refresh, 10);
        return data;
      });
    },
    pop() {
      if (confirm("**Import** Do you really want to apply id?")) {
        var r = this;
        var url = config.APP.ApiUrl + 'api/applyid';
        return Ember.$.ajax({
          method: 'post',
          url: url,
          crossDomain: true,
          xhrFields: {
            withCredentials: true
          }
        }).then(function(data) {
          Ember.run.later(r, r.refresh, 10);
          alert("applyed :" + data.status);
          return data;
        });
      }
    },
    changeAlarm(newValue) {
      console.log(newValue);
      const { selectedAlarm } = this.get('controller').getProperties('selectedAlarm');

      var r = this;
      var url = config.APP.ApiUrl + 'api/changealarm';
      return Ember.$.ajax({
        method: 'post',
        url: url,
        crossDomain: true,
        xhrFields: {
          withCredentials: true
        },
        data: JSON.stringify({
          ip: this.focusUsername,
          alarm: selectedAlarm
        })
      }).then(function(data) {
        alert("Your alarm has been changed");
        Ember.run.later(r, r.refresh, 10);
        return data;
      });
    },
    changeDesc(newValue) {
      console.log(newValue);
      const { selectedDesc } = this.get('controller').getProperties('selectedDesc');
      if (selectedDesc.match(/[0-9A-Za-z]{1,40}/g) === false) {
        alert("entered character and numberic");
        return false;
      }

      var r = this;
      var url = config.APP.ApiUrl + 'api/changedesc';
      return Ember.$.ajax({
        method: 'post',
        url: url,
        crossDomain: true,
        xhrFields: {
          withCredentials: true
        },
        data: JSON.stringify({
          ip: this.focusUsername,
          desc: selectedDesc
        })
      }).then(function(data) {
        alert("Your desc has been changed");
        Ember.run.later(r, r.refresh, 10);
        return data;
      });
    },
  },

	model: function() {
    var url = config.APP.ApiUrl + 'api/idbounds';
    return Ember.$.ajax({
      method: 'get',
      url: url,
      crossDomain: true,
      xhrFields: {
        withCredentials: true
      },
    }).then(function(data) {
			return data;
    });
	},

  beforeModel() {
    if(!this.get('auth').isLoggedIn()) {
        this.transitionTo('login');
    }
  },
});
