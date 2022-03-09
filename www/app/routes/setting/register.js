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
    add(newValue,newPass) {
      console.log(newValue);
      var r = this;
      const { textID, textPass, textPassConfirm } = this.get('controller').getProperties('textID', 'textPass', 'textPassConfirm');
      if (textPass && textID) {
        if (textPass != textPassConfirm) {
          alert("password entered is different")
          return;
        }

        var url = config.APP.ApiUrl + 'api/addaccount';
        return Ember.$.ajax({
          method: 'post',
          url: url,
          crossDomain: true,
          xhrFields: {
            withCredentials: true
          },
          data: JSON.stringify({
            username: textID,
            password: textPass,
          })
        }).then(function(data) {
          Ember.run.later(r, r.refresh, 10);
          return data;
        });
      }
    },
    delete(newValue) {
      console.log(newValue);
      var r = this;
      var url = config.APP.ApiUrl + 'api/delaccount';
      return Ember.$.ajax({
        method: 'post',
        url: url,
        crossDomain: true,
        xhrFields: {
          withCredentials: true
        },
        data: JSON.stringify({
          username: newValue,
        })
      }).then(function(data) {
        return data;
      });
    },
    changeAccess(newValue) {
      console.log(newValue);
      const { selectedAccess } = this.get('controller').getProperties('selectedAccess');

      var r = this;
      var url = config.APP.ApiUrl + 'api/changeacc';
      return Ember.$.ajax({
        method: 'post',
        url: url,
        crossDomain: true,
        xhrFields: {
          withCredentials: true
        },
        data: JSON.stringify({
          username: this.focusUsername,
          access: selectedAccess
        })
      }).then(function(data) {
        alert("Your access has been changed");
        Ember.run.later(r, r.refresh, 10);
        return data;
      });
    },
    changePass(newValue) {
      console.log(newValue);
      const { textChangePass, textChangePassConfirm } = this.get('controller').getProperties('textChangePass', 'textChangePassConfirm');

      if (this.focusUsername && textChangePass) {
        if (textChangePass != textChangePassConfirm) {
          alert("password entered is different")
          return;
        }
        var r = this;
        var url = config.APP.ApiUrl + 'api/changepass';
        return Ember.$.ajax({
          method: 'post',
          url: url,
          crossDomain: true,
          xhrFields: {
            withCredentials: true
          },
          data: JSON.stringify({
            username: this.focusUsername,
            password: textChangePass
          })
        }).then(function(data) {
          alert("Your password has been changed");
          Ember.run.later(r, r.refresh, 10);
          return data;
        });
      }
    },
  },


	model: function() {
    var url = config.APP.ApiUrl + 'api/reglist';
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
