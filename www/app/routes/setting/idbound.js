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
      const { textID, selectedRule } = this.get('controller').getProperties('textID', 'selectedRule');
      if (!Ember.isEmpty(textID)) {
        var result = textID.match(/\b0x[0-9A-Fa-f]{40}|[0-9A-Fa-f]{40}\b/g);
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
