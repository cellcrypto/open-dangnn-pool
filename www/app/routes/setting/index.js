import Ember from 'ember';
import config from '../../config/environment';

export default Ember.Route.extend({
  auth: Ember.inject.service('auth'),

  isHex: function() {
    Ember.run.later(this, this.refresh, 10);
  },

  actions: {
    error: function(reason, transition) {
      this.transitionTo('/login');
      console.log(reason + transition);
      return false;
    },
    search(devid) {
      //devid = devid.replace(/\d+$/, function(n){ return ++n });
      if (!Ember.isEmpty(devid)) {
        var result = devid.match(/\b[0-9A-Fa-f]{1,40}\b/g);
        console.log(result,devid);
        if (result) {
          localStorage.setItem('devid',result);
          Ember.run.later(this, this.refresh, 10);
        }
      }
    },
  },


	model: function() {
    var devid = localStorage.getItem('devid');
    if (devid) {
      var url = config.APP.ApiUrl + 'api/devsearch';

      return Ember.$.ajax({
        method: 'post',
        url: url,
        crossDomain: true,
        xhrFields: {
          withCredentials: true
        },
        data: JSON.stringify({
          devid: devid,
        })
      }).then(function(data) {
        return data;
      });
    } else {
      return ;
    }
	},

  beforeModel() {
    if(!this.get('auth').isLoggedIn()) {
        this.transitionTo('login');
    }
  },

  // setupController: function(controller, model) {
  //   this._super(controller, model);
  //   Ember.run.later(this, this.refresh, 5000);
  // }
});
