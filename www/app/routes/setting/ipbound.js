import Ember from 'ember';
import config from '../../config/environment';

export default Ember.Route.extend({
  auth: Ember.inject.service('auth'),

  actions: {
    error: function(reason, transition) {
      this.transitionTo('/login');
      console.log(reason + transition);
      return false;
    },
    add(newValue) {
      console.log(newValue);
      var r = this;
      const { textIp, selectedRule } = this.get('controller').getProperties('textIp', 'selectedRule');
      var url = config.APP.ApiUrl + 'api/saveinbound';
      return Ember.$.ajax({
        method: 'post',
        url: url,
        crossDomain: true,
        xhrFields: {
          withCredentials: true
        },
        data: JSON.stringify({
          ip: textIp,
          rule: selectedRule,
        })
      }).then(function(data) {
        Ember.run.later(r, r.refresh, 10);
        return data;
      });
    },
    delete(newValue) {
      console.log(newValue);
      if (confirm("["+newValue+"]\n**Import** Do you really want to delete ip?")) {
        var r = this;
        var url = config.APP.ApiUrl + 'api/delinbound';
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
      }
    },
    pop() {
      if (confirm("**Import** Do you really want to apply ip?")) {
        var r = this;
        var url = config.APP.ApiUrl + 'api/applyip';
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
  },


	model: function() {
    var url = config.APP.ApiUrl + 'api/inbounds';
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
